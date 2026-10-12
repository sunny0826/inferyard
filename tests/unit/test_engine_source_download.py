"""In-process engine acquisition against a real loopback HTTP server."""

import gzip
import hashlib
import json
import threading
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.engine_source import EngineSource
from inferyard.platforms.engine_source_download import acquire
from tests.unit.test_engine_acquire_cli import SOURCE

PAYLOAD = b"synthetic engine archive bytes\n" * 64
FILENAME = "llama-prism-b10743-adfffbe-bin-macos-arm64.tar.gz"


@contextmanager
def body_server(mode="ok"):
    requests = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            requests.append(
                (self.path, self.headers.get("Authorization"), self.headers.get("Accept-Encoding"))
            )
            if mode == "redirect" and self.path == f"/{FILENAME}":
                self.send_response(302)
                self.send_header("Location", f"/cdn/{FILENAME}")
                self.send_header("Content-Length", "0")
                self.end_headers()
                return
            if mode == "compressed" and self.path == f"/{FILENAME}":
                body = gzip.compress(PAYLOAD)
                self.send_response(200)
                self.send_header("Content-Encoding", "gzip")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                try:
                    self.wfile.write(body)
                except OSError:
                    pass
                return
            if mode == "missing" or self.path not in (f"/{FILENAME}", f"/cdn/{FILENAME}"):
                self.send_response(404)
                self.send_header("Content-Length", "0")
                self.end_headers()
                return
            self.send_response(200)
            self.send_header("Content-Length", str(len(PAYLOAD)))
            self.end_headers()
            try:
                self.wfile.write(PAYLOAD)
            except OSError:
                pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.02})
    thread.start()
    try:
        yield server, requests
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=1)
        assert not thread.is_alive()


def loopback(monkeypatch, port):
    monkeypatch.setattr(
        EngineSource,
        "download_url",
        property(lambda self: f"http://127.0.0.1:{port}/{FILENAME}"),
    )


def test_download_streams_bytes_and_records_identity(tmp_path, monkeypatch):
    with body_server() as (server, requests):
        loopback(monkeypatch, server.server_port)
        details = acquire(SOURCE, tmp_path / "new")
    out = tmp_path / "new"
    assert (out / FILENAME).read_bytes() == PAYLOAD
    assert details["bytes"] == len(PAYLOAD)
    assert details["sha256"] == hashlib.sha256(PAYLOAD).hexdigest()
    assert details["ready_to_run"] is False
    assert details["engine"] == str(out / FILENAME)
    assert details["source_record"] == str(out / "engine-source.json")
    record = json.loads((out / "engine-source.json").read_text())
    assert record == {
        "definition": "remote_engine_source.v1",
        "parser": "remote-engine-source-parser.v1",
        "source_url": SOURCE,
        "bytes": len(PAYLOAD),
        "sha256": hashlib.sha256(PAYLOAD).hexdigest(),
        "retrieved_at": record["retrieved_at"],
    }
    assert len(requests) == 1 and requests[0][1] is None and requests[0][2] == "identity"


def test_compressed_response_is_rejected_instead_of_published(tmp_path, monkeypatch):
    with body_server("compressed") as (server, requests):
        loopback(monkeypatch, server.server_port)
        with pytest.raises(PreparationError, match="^engine_acquire_incomplete$"):
            acquire(SOURCE, tmp_path / "new")
    assert requests[0][2] == "identity"
    assert not (tmp_path / "new" / FILENAME).exists()
    assert not (tmp_path / "new" / "engine-source.json").exists()


def test_redirect_is_followed_without_extra_configuration(tmp_path, monkeypatch):
    with body_server("redirect") as (server, requests):
        loopback(monkeypatch, server.server_port)
        details = acquire(SOURCE, tmp_path / "new")
    assert details["bytes"] == len(PAYLOAD)
    assert [path for path, *_ in requests] == [f"/{FILENAME}", f"/cdn/{FILENAME}"]


def test_http_failure_keeps_failed_directory_without_outputs(tmp_path, monkeypatch):
    with body_server("missing") as (server, _):
        loopback(monkeypatch, server.server_port)
        with pytest.raises(PreparationError, match="^engine_acquire_incomplete$") as caught:
            acquire(SOURCE, tmp_path / "new")
    assert caught.value.code == 2
    assert (tmp_path / "new").is_dir()
    assert not (tmp_path / "new" / FILENAME).exists()
    assert not (tmp_path / "new" / "engine-source.json").exists()
    assert not list((tmp_path / "new").glob("*.part"))


def test_existing_output_is_rejected_before_any_request(tmp_path, monkeypatch):
    (tmp_path / "new").mkdir()
    with body_server() as (server, requests):
        loopback(monkeypatch, server.server_port)
        with pytest.raises(PreparationError, match="^output_exists$"):
            acquire(SOURCE, tmp_path / "new")
    assert requests == []


@pytest.mark.parametrize("seconds", [0, -1, True, float("nan"), float("inf"), "1800"])
def test_timeout_must_be_a_positive_finite_number(tmp_path, seconds):
    with pytest.raises(PreparationError, match="^invalid_engine_source$"):
        acquire(SOURCE, tmp_path / "new", timeout_seconds=seconds)
