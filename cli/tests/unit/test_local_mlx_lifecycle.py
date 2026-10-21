"""Formal HTTP completion, cancellation and poisoned-state regression checks."""

import json
import socket
import struct
import threading
from concurrent.futures import ThreadPoolExecutor

import pytest

from tests.unit.test_engine_fit_mlx_server import SyntheticBackend, request, response, until
from tests.unit.test_local_mlx_server import payload, server


def test_ordinary_response_waits_for_generator_close_and_gpu_sync():
    closed, syncing, release = (threading.Event() for _ in range(3))

    def produce(_prompt, _tokens):
        try:
            yield response()
        finally:
            closed.set()

    def sync():
        syncing.set()
        assert release.wait(3)

    with server(SyntheticBackend(produce, sync)) as instance, ThreadPoolExecutor() as executor:
        result = executor.submit(request, instance, "POST", "/v1/chat/completions", payload())
        try:
            assert syncing.wait(3) and closed.is_set()
            assert not result.done()
            assert instance.generations.metrics() == (1, 0, 0)
        finally:
            release.set()
        assert result.result(timeout=3)[0] == 200
        assert instance.generations.metrics() == (0, 0, 0)


@pytest.mark.parametrize("stream", [False, True])
@pytest.mark.parametrize("failure", ["generation", "close", "sync", "missing_finish", "oversized"])
def test_failure_never_returns_success_terminal_and_blocks_next_request(stream, failure):
    class Source:
        def __iter__(self):
            if failure == "generation":
                raise RuntimeError("private backend detail")
            text = "x" * (4 * 1024**2 + 1) if failure == "oversized" else "42"
            yield response(text, finish=None if failure == "missing_finish" else "stop")

        def close(self):
            if failure == "close":
                raise RuntimeError("close uncertain")

    def sync():
        if failure == "sync":
            raise RuntimeError("sync uncertain")

    backend = SyntheticBackend(synchronize=sync)
    backend.responses = lambda _prompt, _tokens: Source()
    with server(backend) as instance:
        status, raw = request(instance, "POST", "/v1/chat/completions", payload(stream))
        assert b"private backend detail" not in raw
        if stream:
            assert status == 200 and b"[DONE]" not in raw and b'"usage"' not in raw
        else:
            assert status == 500 and json.loads(raw) == {"error": "generation_failed"}
        assert instance.generations.metrics() == (int(failure in ("close", "sync")), 0, 1)
        assert request(instance, "POST", "/v1/chat/completions", payload())[0] == 503


@pytest.mark.parametrize("stream", [False, True])
def test_tcp_disconnect_preserves_running_until_sync_and_poison_after_idle(stream):
    entered, release, syncing, release_sync = (threading.Event() for _ in range(4))

    def produce(_prompt, _tokens):
        entered.set()
        assert release.wait(3)
        yield response()

    def sync():
        syncing.set()
        assert release_sync.wait(3)

    with server(SyntheticBackend(produce, sync)) as instance:
        connection = socket.create_connection(instance.server_address, timeout=3)
        raw = json.dumps(payload(stream)).encode()
        connection.sendall(
            b"POST /v1/chat/completions HTTP/1.1\r\nHost: localhost\r\n"
            b"Content-Type: application/json\r\nContent-Length: "
            + str(len(raw)).encode()
            + b"\r\n\r\n"
            + raw
        )
        try:
            assert entered.wait(3)
            connection.setsockopt(socket.SOL_SOCKET, socket.SO_LINGER, struct.pack("ii", 1, 0))
            connection.close()
            until(lambda: instance.generations.active.cancelled.is_set())
            assert instance.generations.metrics() == (1, 0, 1)
            release.set()
            assert syncing.wait(3)
            assert instance.generations.metrics() == (1, 0, 1)
        finally:
            connection.close()
            release.set()
            release_sync.set()
        until(lambda: instance.generations.metrics() == (0, 0, 1))
        assert request(instance, "POST", "/v1/chat/completions", payload())[0] == 503


def test_http_queue_capacity_rejection_does_not_poison_active_work():
    entered, release = threading.Event(), threading.Event()

    def produce(_prompt, _tokens):
        entered.set()
        assert release.wait(3)
        yield response()

    from tests.unit.test_engine_fit_mlx_server import drain

    with server(SyntheticBackend(produce)) as instance:
        first = instance.generations.submit("active", 8)
        try:
            assert entered.wait(3)
            waiting = [instance.generations.submit("queued", 8) for _ in range(4)]
            status, raw = request(instance, "POST", "/v1/chat/completions", payload())
            assert status == 429 and json.loads(raw) == {"error": "generation_queue_full"}
            assert instance.generations.metrics() == (1, 4, 0)
        finally:
            release.set()
        for job in [first, *waiting]:
            assert drain(job)[-1][0] == "done"


def test_failed_ordinary_delivery_after_sync_still_poisons(monkeypatch):
    import http.client

    from tests.unit.test_local_mlx_server import formal

    original = formal.Handler._reply

    def fail_delivery(self, status, body, content_type="application/json"):
        if status == 200 and isinstance(body, dict) and "choices" in body:
            assert self.server.generations.metrics() == (0, 0, 0)
            raise BrokenPipeError("synthetic post-sync disconnect")
        return original(self, status, body, content_type)

    monkeypatch.setattr(formal.Handler, "_reply", fail_delivery)
    with server() as instance:
        with pytest.raises(http.client.RemoteDisconnected):
            request(instance, "POST", "/v1/chat/completions", payload())
        until(lambda: instance.generations.metrics() == (0, 0, 1))
        assert request(instance, "POST", "/v1/chat/completions", payload())[0] == 503
