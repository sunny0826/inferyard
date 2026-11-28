import asyncio
import json
import ssl
from types import SimpleNamespace

import pytest

from inferyard.contracts.node_protocol import (
    HANDSHAKE_CONTROL_LINE,
    HANDSHAKE_TUNNEL_PREFIX,
    decode_ndjson_frame,
    encode_ndjson_frame,
)
from inferyard.node.agent import NodeAgent
from inferyard.node.credentials import save_pairing
from inferyard.node.tunnel import TunnelRejected, open_tunnel
from tests.integration.test_node_pairing import certificates, server_context
from tests.integration.test_runner import scenario as runner_scenario

scenario = runner_scenario


def make_state(tmp_path, paths, port):
    response = {
        "node_id": "node-test",
        "ca_cert_pem": paths["ca"].read_text(),
        "client_cert_pem": paths["cert"].read_text(),
        "client_key_pem": paths["key"].read_text(),
        "hub_addrs": [f"127.0.0.1:{port}"],
    }
    state = tmp_path / "agent-state"
    save_pairing(state, response, ca_sha256=paths["fingerprint"], autostart_mode="none")
    return state


def authenticated_server_context(paths):
    context = server_context(paths)
    context.verify_mode = ssl.CERT_REQUIRED
    context.load_verify_locations(cafile=str(paths["ca"]))
    return context


def test_mtls_control_ping_and_rpc(tmp_path, monkeypatch):
    monkeypatch.setattr(
        "inferyard.node.join.device_snapshot",
        lambda: {"cpu_model": "test-cpu", "logical_cpus": 8},
    )

    async def scenario():
        paths = certificates(tmp_path)
        observed = {}
        finished = asyncio.get_running_loop().create_future()

        async def handler(reader, writer):
            assert await reader.readline() == HANDSHAKE_CONTROL_LINE.encode()
            hello = decode_ndjson_frame(await reader.readline())
            observed["hello"] = hello
            writer.write(b'{"event":"ping","data":{"sequence":1}}\n')
            await writer.drain()
            observed["pong"] = decode_ndjson_frame(await reader.readline())
            writer.write(b'{"id":"rpc-1","method":"service.list","params":{}}\n')
            await writer.drain()
            observed["rpc"] = decode_ndjson_frame(await reader.readline())
            finished.set_result(True)
            writer.close()
            await writer.wait_closed()

        server = await asyncio.start_server(
            handler, "127.0.0.1", 0, ssl=authenticated_server_context(paths)
        )
        port = server.sockets[0].getsockname()[1]
        agent = NodeAgent(make_state(tmp_path, paths, port))
        task = asyncio.create_task(agent.run_once(f"127.0.0.1:{port}"))
        await asyncio.wait_for(finished, 10)
        await asyncio.wait_for(task, 10)
        assert observed["hello"]["data"]["node_id"] == "node-test"
        assert json.loads(observed["hello"]["data"]["hardware_summary"])["cpu_model"] == "test-cpu"
        assert observed["hello"]["data"]["device_snapshot"]["logical_cpus"] == 8
        assert observed["pong"] == {"event": "pong", "data": {"sequence": 1}}
        assert observed["rpc"] == {"id": "rpc-1", "ok": True, "result": {"services": []}}
        await agent.close()
        server.close()
        await server.wait_closed()

    asyncio.run(scenario())


def test_tunnel_bidirectional_sse_bytes_and_port_rejection(tmp_path):
    payload = b'data: {"token":"hello"}\n\ndata: [DONE]\n\n'

    async def scenario():
        paths = certificates(tmp_path)

        async def echo(reader, writer):
            data = await reader.readexactly(len(payload))
            writer.write(data)
            await writer.drain()
            writer.close()
            await writer.wait_closed()

        local = await asyncio.start_server(echo, "127.0.0.1", 0)
        local_port = local.sockets[0].getsockname()[1]
        received_all = asyncio.get_running_loop().create_future()

        async def hub(reader, writer):
            handshake = await reader.readline()
            if handshake == HANDSHAKE_CONTROL_LINE.encode():
                decode_ndjson_frame(await reader.readline())
                event = {
                    "event": "open_tunnel",
                    "data": {"stream_id": "stream-1", "target_port": local_port, "service_id": "allowed"},
                }
                writer.write((json.dumps(event) + "\n").encode())
                await writer.drain()
                await reader.read()
                return
            assert handshake == f"{HANDSHAKE_TUNNEL_PREFIX}stream-1\n".encode()
            writer.write(payload)
            await writer.drain()
            # Read until EOF (verifies engine connection-close EOF is forwarded over TLS).
            data = await reader.read()
            if not received_all.done():
                received_all.set_result(data)
            writer.close()
            await writer.wait_closed()

        server = await asyncio.start_server(
            hub, "127.0.0.1", 0, ssl=authenticated_server_context(paths)
        )
        port = server.sockets[0].getsockname()[1]
        agent = NodeAgent(make_state(tmp_path, paths, port))
        agent.supervisor.allowed_ports.add(local_port)
        task = asyncio.create_task(agent.run_once(f"127.0.0.1:{port}"))
        assert await asyncio.wait_for(received_all, 10) == payload
        await agent.close()
        await asyncio.wait_for(task, 10)
        local.close()
        server.close()
        await local.wait_closed()
        await server.wait_closed()

        with pytest.raises(TunnelRejected):
            await open_tunnel(
                state_dir=agent.state_dir,
                hub=f"127.0.0.1:{port}",
                stream_id="forbidden",
                target_port=1,
                service_id="unknown",
                supervisor=agent.supervisor,
            )

    asyncio.run(scenario())


def test_oversized_rpc_result_returns_error_frame():
    async def scenario():
        async def call(method, params):
            return {"blob": "x" * (16 * 1024 * 1024)}

        agent = NodeAgent.__new__(NodeAgent)
        agent.rpc = SimpleNamespace(call=call)
        sent = []

        async def send(frame):
            encode_ndjson_frame(frame)
            sent.append(frame)

        agent._send = send
        await agent._answer({"id": "large", "method": "report.export", "params": {}})
        assert sent == [
            {
                "id": "large",
                "ok": False,
                "error": {
                    "code": "response_too_large",
                    "message": "control frame exceeds maximum allowed size",
                },
            }
        ]

    asyncio.run(scenario())


def test_interrupted_batch_report_export_marks_resumable_run(tmp_path, scenario):
    from pathlib import Path

    from inferyard.application.execution import execute_async
    from inferyard.node.report_export import export_report
    from tests.integration.test_batch_runner import batch

    request, deps = batch(tmp_path, scenario)

    async def cancel_batch():
        task = asyncio.create_task(execute_async(request, deps))
        while len(scenario[2]) < 1 and not task.done():
            await asyncio.sleep(0.001)
        task.cancel()
        return await task

    code, result = asyncio.run(cancel_batch())
    assert code == 130
    interrupted_run = Path(result.details["runs"][0]).resolve()
    exported = export_report(
        state_dir=tmp_path / "state",
        run_path=str(interrupted_run),
        out=str(tmp_path / "exported"),
    )
    assert exported["resumable_run_paths"] == [str(interrupted_run)]
    assert exported["rerunnable_run_paths"] == []
