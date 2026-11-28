"""On-demand mTLS tunnel streams connected only to approved loopback services."""

from __future__ import annotations

import asyncio
from pathlib import Path

from inferyard.contracts.node_protocol import MAX_CONTROL_LINE_BYTES, format_tunnel_handshake
from inferyard.node.join import parse_hub
from inferyard.node.tls import control_context


class TunnelRejected(PermissionError):
    pass


async def _pump(reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
    try:
        while True:
            data = await reader.read(65536)
            if not data:
                break
            writer.write(data)
            await writer.drain()
        if writer.can_write_eof():
            writer.write_eof()
            await writer.drain()
        else:
            writer.close()
    except (ConnectionError, asyncio.IncompleteReadError, OSError):
        pass


async def open_tunnel(
    *,
    state_dir: Path,
    hub: str,
    stream_id: str,
    target_port: int,
    service_id: str | None,
    supervisor,
) -> None:
    if not supervisor.allows_port(target_port, service_id):
        raise TunnelRejected("target port is not a managed or explicitly allowed loopback port")
    handshake = format_tunnel_handshake(stream_id)
    host, port = parse_hub(hub)
    local_reader, local_writer = await asyncio.open_connection("127.0.0.1", target_port)
    try:
        hub_reader, hub_writer = await asyncio.open_connection(
            host,
            port,
            ssl=control_context(state_dir),
            server_hostname=host,
            limit=MAX_CONTROL_LINE_BYTES + 1,
        )
    except BaseException:
        local_writer.close()
        await local_writer.wait_closed()
        raise
    try:
        hub_writer.write(handshake.encode("ascii"))
        await hub_writer.drain()
        await asyncio.gather(
            _pump(local_reader, hub_writer),
            _pump(hub_reader, local_writer),
        )
    finally:
        for writer in (local_writer, hub_writer):
            writer.close()
        await asyncio.gather(
            local_writer.wait_closed(), hub_writer.wait_closed(), return_exceptions=True
        )
