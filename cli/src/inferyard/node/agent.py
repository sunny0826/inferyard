"""Resident mTLS node control loop with reconnect, RPC and on-demand tunnels."""

from __future__ import annotations

import asyncio
import platform
import socket
from pathlib import Path

from inferyard import __version__
from inferyard.contracts import node_protocol as protocol
from inferyard.node.credentials import load_config
from inferyard.node.job_runner import JobRunner
from inferyard.node.join import normalize_hubs, normalized_os_arch, parse_hub
from inferyard.node.rpc import NodeRPC, RPCError
from inferyard.node.service_supervisor import ServiceSupervisor
from inferyard.node.tls import control_context
from inferyard.node.tunnel import open_tunnel


class NodeAgent:
    def __init__(self, state_dir: Path, *, hubs=None):
        self.state_dir = state_dir
        self.config = load_config(state_dir)
        self.hubs = normalize_hubs(hubs or self.config["hub_addrs"])
        self.writer: asyncio.StreamWriter | None = None
        self.write_lock = asyncio.Lock()
        self.pending_events: asyncio.Queue[dict] = asyncio.Queue(maxsize=1024)
        self.supervisor = ServiceSupervisor(
            state_dir,
            allowed_ports=self.config.get("allowed_tunnel_ports", ()),
            event_sink=self.queue_event,
        )
        self.jobs = JobRunner(state_dir, self.supervisor, event_sink=self.queue_event)
        self.rpc = NodeRPC(state_dir, self.supervisor, self.jobs)
        self.tasks: set[asyncio.Task] = set()
        self.tunnels: set[asyncio.Task] = set()
        self.stopping = False

    def _track(self, coroutine, *, tunnel: bool = False) -> asyncio.Task:
        task = asyncio.create_task(coroutine)
        target = self.tunnels if tunnel else self.tasks
        target.add(task)
        task.add_done_callback(target.discard)
        return task

    async def queue_event(self, event: str, data: dict) -> None:
        frame = {"event": event, "data": data}
        if self.writer is not None:
            try:
                await self._send(frame)
                return
            except (ConnectionError, OSError):
                pass
        if self.pending_events.full():
            self.pending_events.get_nowait()
        self.pending_events.put_nowait(frame)

    async def _send(self, frame: dict) -> None:
        if self.writer is None:
            raise ConnectionError("control connection is not active")
        async with self.write_lock:
            self.writer.write(protocol.encode_ndjson_frame(frame))
            await self.writer.drain()

    def _hello(self) -> dict:
        os_name, arch, plat = normalized_os_arch()
        return {
            "event": protocol.EVENT_HELLO,
            "data": {
                "node_id": self.config["node_id"],
                "hostname": socket.gethostname(),
                "os": os_name,
                "arch": arch,
                "platform": plat,
                "cli_version": __version__,
                "autostart_installed": (self.state_dir / "autostart.json").exists(),
            },
        }

    async def run_once(self, hub: str) -> None:
        host, port = parse_hub(hub)
        reader, writer = await asyncio.open_connection(
            host,
            port,
            ssl=control_context(self.state_dir),
            server_hostname=host,
            limit=protocol.MAX_CONTROL_LINE_BYTES + 1,
        )
        self.writer = writer
        try:
            writer.write(protocol.HANDSHAKE_CONTROL_LINE.encode("ascii"))
            await writer.drain()
            await self._send(self._hello())
            while not self.pending_events.empty():
                await self._send(self.pending_events.get_nowait())
            while not self.stopping:
                line = await reader.readline()
                if not line:
                    break
                frame = protocol.decode_ndjson_frame(line)
                if "method" in frame:
                    self._track(self._answer(frame))
                elif frame.get("event") == protocol.EVENT_PING:
                    await self._send({"event": protocol.EVENT_PONG, "data": frame.get("data", {})})
                elif frame.get("event") == protocol.EVENT_OPEN_TUNNEL:
                    self._open_tunnel(hub, frame.get("data") or {})
        finally:
            self.writer = None
            for task in tuple(self.tasks):
                task.cancel()
            await asyncio.gather(*self.tasks, return_exceptions=True)
            writer.close()
            await writer.wait_closed()

    async def _answer(self, frame: dict) -> None:
        request_id = frame.get("id")
        try:
            if request_id is None or isinstance(request_id, (dict, list)):
                raise RPCError("invalid_request", "RPC id is required")
            params = frame.get("params", {})
            if not isinstance(params, dict):
                raise RPCError("invalid_params", "RPC params must be an object")
            result = await self.rpc.call(frame["method"], params)
            response = {"id": request_id, "ok": True, "result": result}
        except RPCError as exc:
            response = {"id": request_id, "ok": False, "error": exc.as_dict()}
        except Exception as exc:
            response = {
                "id": request_id,
                "ok": False,
                "error": {"code": "internal_error", "message": type(exc).__name__},
            }
        try:
            await self._send(response)
        except (ConnectionError, OSError):
            return

    def _open_tunnel(self, hub: str, data: dict) -> None:
        try:
            stream_id = data["stream_id"]
            target_port = data["target_port"]
            service_id = data.get("service_id")
            if not self.supervisor.allows_port(target_port, service_id):
                raise PermissionError("unmanaged tunnel port")
        except (KeyError, TypeError, PermissionError):
            return
        self._track(
            open_tunnel(
                state_dir=self.state_dir,
                hub=hub,
                stream_id=stream_id,
                target_port=target_port,
                service_id=service_id,
                supervisor=self.supervisor,
            ),
            tunnel=True,
        )

    async def run(self) -> None:
        attempt = 0
        while not self.stopping:
            for hub in self.hubs:
                if self.stopping:
                    break
                try:
                    await self.run_once(hub)
                    attempt = 0
                except (ConnectionError, OSError, ValueError):
                    attempt += 1
                if self.stopping:
                    break
                delay = min(60.0, 0.25 * (2 ** min(attempt, 8)))
                await asyncio.sleep(delay)

    async def close(self, *, stop_services: bool = False) -> None:
        self.stopping = True
        if self.writer is not None:
            self.writer.close()
        for task in (*tuple(self.tasks), *tuple(self.tunnels)):
            task.cancel()
        await asyncio.gather(*self.tasks, *self.tunnels, return_exceptions=True)
        await self.jobs.close()
        if stop_services:
            for record in self.supervisor.list():
                if record.get("status") == "running":
                    await self.supervisor.stop(record["service_id"])
        await self.supervisor.close()


async def run_agent(state_dir: Path, hubs=None) -> None:
    agent = NodeAgent(state_dir, hubs=hubs)
    try:
        await agent.run()
    finally:
        await agent.close()
