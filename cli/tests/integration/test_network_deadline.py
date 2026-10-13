"""F06: actual TCP streaming cannot renew the whole-request deadline."""

import asyncio
import json
import time

from inferyard.adapters.prism import PrismAdapter


def test_real_tcp_continuous_stream_stops_at_total_deadline():
    async def exercise():
        tasks = set()
        received = []

        async def handler(reader, writer):
            task = asyncio.current_task()
            tasks.add(task)
            try:
                headers = await reader.readuntil(b"\r\n\r\n")
                size = next(
                    int(line.split(b":", 1)[1])
                    for line in headers.split(b"\r\n")
                    if line.lower().startswith(b"content-length:")
                )
                received.append(json.loads(await reader.readexactly(size)))
                writer.write(
                    b"HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\n"
                    b"Connection: close\r\n\r\n"
                )
                while True:
                    writer.write(b'data: {"choices":[{"index":0,"delta":{"content":"x"}}]}\n\n')
                    await writer.drain()
                    await asyncio.sleep(0.05)
            except ConnectionError, asyncio.CancelledError:
                pass
            finally:
                writer.close()
                tasks.discard(task)

        server = await asyncio.start_server(handler, "127.0.0.1", 0)
        port = server.sockets[0].getsockname()[1]
        adapter = PrismAdapter(f"http://127.0.0.1:{port}")
        try:
            started = time.monotonic()
            result = await adapter.generate({"stream": True}, 1.0)
            elapsed = time.monotonic() - started
            assert 0.95 <= elapsed <= 1.25
            assert result["execution_state"] == "failed"
            assert result["error_category"] == "total_timeout"
            assert len(result["content"]) >= 10
            assert len(received) == 1
        finally:
            await adapter.close()
            server.close()
            await server.wait_closed()
            pending = list(tasks)
            for task in pending:
                task.cancel()
            await asyncio.gather(*pending, return_exceptions=True)

    asyncio.run(exercise())
