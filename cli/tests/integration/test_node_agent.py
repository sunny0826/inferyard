import asyncio
import json
import ssl

import pytest

from inferyard.contracts.node_protocol import (
    HANDSHAKE_CONTROL_LINE,
    HANDSHAKE_TUNNEL_PREFIX,
    decode_ndjson_frame,
)
from inferyard.node.agent import NodeAgent
from inferyard.node.credentials import save_pairing
from inferyard.node.tunnel import TunnelRejected, open_tunnel
from tests.integration.test_node_pairing import certificates, server_context


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


def test_mtls_control_ping_and_rpc(tmp_path):
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

        local = await asyncio.start_server(echo, "127.0.0.1", 0)
        local_port = local.sockets[0].getsockname()[1]
        echoed = asyncio.get_running_loop().create_future()

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
            data = await reader.readexactly(len(payload))
            if not echoed.done():
                echoed.set_result(data)
            writer.close()
            await writer.wait_closed()

        server = await asyncio.start_server(
            hub, "127.0.0.1", 0, ssl=authenticated_server_context(paths)
        )
        port = server.sockets[0].getsockname()[1]
        agent = NodeAgent(make_state(tmp_path, paths, port))
        agent.supervisor.allowed_ports.add(local_port)
        task = asyncio.create_task(agent.run_once(f"127.0.0.1:{port}"))
        assert await asyncio.wait_for(echoed, 10) == payload
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
