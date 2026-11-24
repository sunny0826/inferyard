"""Formal HTTP completion, cancellation and poisoned-state regression checks."""

import json
import socket
import struct
import threading

import pytest

from tests.unit.test_engine_fit_mlx_server import SyntheticBackend, request, response, until
from tests.unit.test_local_mlx_server import payload, server


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
