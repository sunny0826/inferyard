"""Managed loopback engine processes with identity checks and no crash restart."""

from __future__ import annotations

import asyncio
import json
import os
import re
import socket
import uuid
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import urlsplit

import psutil

from inferyard.contracts.node_protocol import EVENT_SERVICE_EXITED, EVENT_SERVICE_UPDATED
from inferyard.errors import PreflightError
from inferyard.platforms.identity import process_start_ticks

_SERVICE_ID = re.compile(r"^[A-Za-z0-9._-]{1,128}$")


def allocate_port(start: int = 48857) -> int:
    for port in range(start, 65536):
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
            try:
                listener.bind(("127.0.0.1", port))
            except OSError:
                continue
            return port
    raise OSError("no free loopback port")


def log_tail(path: Path, limit: int = 65536) -> str:
    try:
        with path.open("rb") as stream:
            stream.seek(0, os.SEEK_END)
            size = stream.tell()
            stream.seek(max(0, size - limit))
            return stream.read().decode("utf-8", errors="replace")
    except FileNotFoundError:
        return ""


def log_lines(path: Path) -> list[str]:
    return log_tail(path).splitlines()[-80:]


def _timestamp() -> str:
    return datetime.now(UTC).isoformat()


def _set_status(record: dict, status: str) -> None:
    record["state"] = status
    record["status"] = status


def _process_matches(record: dict) -> bool:
    pid, ticks = record.get("pid"), record.get("start_ticks")
    if type(pid) is not int or type(ticks) is not int:
        return False
    try:
        return process_start_ticks(pid) == ticks
    except (OSError, PreflightError, psutil.Error):
        return False


def _postorder(process: psutil.Process) -> list[psutil.Process]:
    result = []
    try:
        children = process.children(recursive=False)
    except psutil.Error:
        return result
    for child in children:
        result.extend(_postorder(child))
    result.append(process)
    return result


def terminate_tree(pid: int, start_ticks: int, *, graceful_timeout: float = 5) -> str | None:
    """Terminate only the verified root, then clean its tree bottom-up after a timeout."""
    try:
        root = psutil.Process(pid)
        if process_start_ticks(pid) != start_ticks:
            return "process_identity_mismatch"
    except (psutil.NoSuchProcess, OSError):
        return None
    forced_reason = None
    try:
        root.terminate()
        root.wait(timeout=graceful_timeout)
    except psutil.TimeoutExpired:
        forced_reason = "graceful_stop_timeout"
    except psutil.NoSuchProcess:
        return None
    if forced_reason:
        for process in _postorder(root):
            try:
                if process.pid == pid and process_start_ticks(pid) != start_ticks:
                    continue
                process.kill()
            except (psutil.NoSuchProcess, OSError):
                continue
        _, alive = psutil.wait_procs(_postorder(root), timeout=3)
        for process in alive:
            try:
                process.kill()
            except psutil.NoSuchProcess:
                pass
    return forced_reason


class ServiceSupervisor:
    def __init__(self, state_dir: Path, *, allowed_ports=(), event_sink=None):
        self.root = state_dir / "services"
        self.root.mkdir(parents=True, exist_ok=True)
        self.allowed_ports = {int(port) for port in allowed_ports}
        self.event_sink = event_sink
        self.services: dict[str, dict] = {}
        self.processes: dict[str, asyncio.subprocess.Process] = {}
        self.monitors: dict[str, asyncio.Task] = {}
        self._load_receipts()

    def _load_receipts(self) -> None:
        for path in self.root.glob("*/receipt.json"):
            try:
                record = json.loads(path.read_text(encoding="utf-8"))
                if not isinstance(record.get("service_id"), str):
                    continue
                record.setdefault("state", record.get("status"))
                if record.get("status") in ("running", "stopping") and not _process_matches(record):
                    self._mark_exited(record)
                self.services[record["service_id"]] = record
            except (OSError, ValueError):
                continue

    def _mark_exited(self, record: dict) -> None:
        _set_status(record, "exited")
        log_path = Path(record.get("log_path") or self.root / record["service_id"] / "engine.log")
        record.update(crashed=True, finished_at=_timestamp(), log_tail=log_lines(log_path))
        self._save(record)

    def _save(self, record: dict) -> None:
        path = self.root / record["service_id"] / "receipt.json"
        temporary = path.with_suffix(".tmp")
        temporary.write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
        os.replace(temporary, path)

    async def _event(self, name: str, record: dict) -> None:
        if self.event_sink is not None:
            await self.event_sink(name, dict(record))

    def _record(self, service_id: str) -> dict:
        if service_id not in self.services:
            raise ValueError("unknown service_id")
        return self.services[service_id]

    def list(self) -> list[dict]:
        return [dict(self.services[key]) for key in sorted(self.services)]

    def allows_port(self, port: int, service_id: str | None = None) -> bool:
        if type(port) is not int or not 1 <= port <= 65535:
            return False
        if port in self.allowed_ports:
            return True
        record = self.services.get(service_id or "")
        if not record or record.get("port") != port or record.get("status") != "running":
            return False
        if not _process_matches(record):
            self._mark_exited(record)
            return False
        return True

    def endpoint(self, service_id: str) -> dict:
        record = self._record(service_id)
        if record.get("status") != "running":
            raise ValueError("service_not_running")
        return {
            "url": f"http://127.0.0.1:{record['port']}",
            "server_pid": record["pid"],
            "process_start_ticks": record["start_ticks"],
        }

    def _prepare_plan(self, params: dict, service_dir: Path, port: int) -> dict:
        from inferyard.application.types import CommandRequest
        from inferyard.config.serve_plan import prepare

        explicit_installation = params.get("installation")
        engine = explicit_installation or params.get("engine_path") or params.get("engine")
        engine_path = Path(engine) if engine else None
        installation = (
            engine_path
            if engine_path is not None
            and (
                explicit_installation
                or engine_path.is_dir()
                or engine_path.name == "installation.json"
            )
            else None
        )
        context_size = params.get("context_size")
        request = CommandRequest(
            "serve prepare",
            model_path=Path(params.get("model_path") or params["model"]),
            installation=installation,
            engine_path=None if installation is not None else engine_path,
            runtime_receipt=(
                Path(params["runtime_receipt"]) if params.get("runtime_receipt") else None
            ),
            context_size=8192 if context_size is None else context_size,
            port=port,
            out=service_dir,
        )
        return prepare(request)

    async def start(self, params: dict) -> dict:
        service_id = params.get("service_id") or uuid.uuid4().hex
        if not _SERVICE_ID.fullmatch(service_id) or service_id in self.services:
            raise ValueError("invalid or duplicate service_id")
        service_dir = self.root / service_id
        service_dir.mkdir(parents=True, exist_ok=False)
        plan = None
        if params.get("serve_plan"):
            plan = json.loads(Path(params["serve_plan"]).read_text(encoding="utf-8"))
        elif isinstance(params.get("plan"), dict):
            plan = params["plan"]
        port = params.get("port")
        if port is None and plan is not None:
            port = urlsplit(plan.get("endpoint", "")).port
        port = port or allocate_port()
        if type(port) is not int or not 1 <= port <= 65535:
            raise ValueError("invalid service port")
        if plan is None:
            plan = self._prepare_plan(params, service_dir, port)
        argv = plan.get("argv")
        if not isinstance(argv, list) or not argv or any(not isinstance(x, str) for x in argv):
            raise ValueError("serve plan has invalid argv")
        plan_port = urlsplit(plan.get("endpoint", "")).port
        if plan_port not in (None, port):
            raise ValueError("serve plan port mismatch")
        log_path = service_dir / "engine.log"
        environment = os.environ.copy()
        environment.update(plan.get("environment") or {})
        with log_path.open("ab") as output:
            process = await asyncio.create_subprocess_exec(
                *argv,
                cwd=plan.get("working_directory") or None,
                env=environment,
                stdin=asyncio.subprocess.DEVNULL,
                stdout=output,
                stderr=asyncio.subprocess.STDOUT,
                start_new_session=os.name != "nt",
                creationflags=(0x00000008 | 0x08000000) if os.name == "nt" else 0,
            )
        try:
            start_ticks = process_start_ticks(process.pid)
        except PreflightError as exc:
            if str(exc) != "service_process_unavailable":
                raise
            start_ticks = None
        endpoint_url = plan.get("endpoint") or f"http://127.0.0.1:{port}"
        engine = plan.get("family") or params.get("engine") or params.get("engine_path") or ""
        record = {
            "service_id": service_id,
            "pid": process.pid,
            "start_ticks": start_ticks,
            "port": port,
            "log_path": str(log_path),
            "started_at": _timestamp(),
            "finished_at": None,
            "endpoint_url": endpoint_url,
            "endpoint": endpoint_url,
            "model": plan.get("model_path") or params.get("model_path") or params.get("model") or "",
            "engine": engine,
            "family": plan.get("family") or engine,
            "adapter": plan.get("adapter"),
            "context_size": plan.get("context_size"),
            "exit_code": None,
            "crashed": False,
            "forced_termination_reason": None,
        }
        _set_status(record, "running" if start_ticks is not None else "exited")
        self.services[service_id] = record
        self.processes[service_id] = process
        if start_ticks is None:
            record["exit_code"] = await process.wait()
            record["log_tail"] = log_lines(log_path)
            record["crashed"] = True
            record["finished_at"] = _timestamp()
        else:
            self.monitors[service_id] = asyncio.create_task(self._monitor(service_id))
        self._save(record)
        event = EVENT_SERVICE_UPDATED if start_ticks is not None else EVENT_SERVICE_EXITED
        await self._event(event, record)
        return dict(record)

    async def _monitor(self, service_id: str) -> None:
        process = self.processes[service_id]
        exit_code = await process.wait()
        record = self.services[service_id]
        status = "stopped" if record["status"] in ("stopping", "stopped") else "exited"
        _set_status(record, status)
        record.update(
            exit_code=exit_code,
            log_tail=log_lines(Path(record["log_path"])),
            crashed=status == "exited",
            finished_at=_timestamp(),
        )
        self._save(record)
        await self._event(EVENT_SERVICE_EXITED, record)

    async def stop(self, service_id: str, *, timeout: float = 5) -> dict:
        record = self._record(service_id)
        _set_status(record, "stopping")
        self._save(record)
        reason = await asyncio.to_thread(
            terminate_tree, record["pid"], record["start_ticks"], graceful_timeout=timeout
        )
        record["forced_termination_reason"] = reason
        process = self.processes.get(service_id)
        if process is not None and process.returncode is None:
            try:
                await asyncio.wait_for(process.wait(), timeout=3)
            except TimeoutError:
                pass
        _set_status(record, "stopped")
        record.update(
            exit_code=process.returncode if process is not None else None,
            log_tail=log_lines(Path(record["log_path"])),
            crashed=False,
            finished_at=_timestamp(),
        )
        self._save(record)
        await self._event(EVENT_SERVICE_UPDATED, record)
        return dict(record)

    def logs(self, service_id: str, *, limit: int = 65536, max_lines: int | None = None) -> dict:
        record = self._record(service_id)
        text = log_tail(Path(record["log_path"]), min(max(limit, 1), 1024 * 1024))
        if max_lines and max_lines > 0:
            text = "\n".join(text.splitlines()[-max_lines:])
        return {"service_id": service_id, "logs": text}

    async def close(self) -> None:
        for task in self.monitors.values():
            task.cancel()
        await asyncio.gather(*self.monitors.values(), return_exceptions=True)
