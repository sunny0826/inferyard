"""In-process acquisition against a real loopback HTTP server."""

import gzip
import hashlib
import json
import threading
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms.model_source import ModelSource
from inferyard.platforms.model_source_download import acquire
from tests.unit.test_model_acquire_cli import SOURCE

PAYLOAD = b"synthetic gguf bytes\n" * 64
FILENAME = "stories260K.gguf"


@contextmanager
def body_server(mode="ok"):
    requests = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            requests.append(
                (
                    self.path,
                    self.headers.get("Authorization"),
                    self.headers.get("Accept-Encoding"),
                )
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
        ModelSource,
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
    assert details["model_requests_sent"] == 0
    record = json.loads((out / "source.json").read_text())
    assert record["definition"] == "remote_model_source.v3"
    assert record["bytes"] == len(PAYLOAD)
    assert record["sha256"] == details["sha256"]
    assert "metadata_sha256" not in record and "acquisition_budget" not in record
    assert len(requests) == 1 and requests[0][1] is None and requests[0][2] == "identity"


def test_compressed_response_is_rejected_instead_of_published(tmp_path, monkeypatch):
    with body_server("compressed") as (server, requests):
        loopback(monkeypatch, server.server_port)
        with pytest.raises(PreparationError, match="^model_acquire_incomplete$"):
            acquire(SOURCE, tmp_path / "new")
    assert requests[0][2] == "identity"
    assert not (tmp_path / "new" / FILENAME).exists()
    assert not (tmp_path / "new" / "source.json").exists()


def test_redirect_is_followed_without_extra_configuration(tmp_path, monkeypatch):
    with body_server("redirect") as (server, requests):
        loopback(monkeypatch, server.server_port)
        details = acquire(SOURCE, tmp_path / "new")
    assert details["bytes"] == len(PAYLOAD)
    assert [path for path, *_ in requests] == [f"/{FILENAME}", f"/cdn/{FILENAME}"]


def test_token_env_value_goes_to_the_request_header(tmp_path, monkeypatch):
    monkeypatch.setenv("HF_TOKEN", "token-value")
    with body_server() as (server, requests):
        loopback(monkeypatch, server.server_port)
        acquire(SOURCE, tmp_path / "new", "HF_TOKEN")
    assert requests[0][1] == "Bearer token-value"


def test_http_failure_keeps_failed_directory_without_outputs(tmp_path, monkeypatch):
    with body_server("missing") as (server, _):
        loopback(monkeypatch, server.server_port)
        with pytest.raises(PreparationError, match="^model_acquire_incomplete$") as caught:
            acquire(SOURCE, tmp_path / "new")
    assert caught.value.code == 2
    assert (tmp_path / "new").is_dir()
    assert not (tmp_path / "new" / FILENAME).exists()
    assert not (tmp_path / "new" / "source.json").exists()
    assert not list((tmp_path / "new").glob("*.part"))


def test_existing_output_is_rejected_before_any_request(tmp_path, monkeypatch):
    (tmp_path / "new").mkdir()
    with body_server() as (server, requests):
        loopback(monkeypatch, server.server_port)
        with pytest.raises(PreparationError, match="^output_exists$"):
            acquire(SOURCE, tmp_path / "new")
    assert requests == []


@pytest.mark.parametrize("token_env", ["literal-token!", "NAME=SECRET", "", "1NAME"])
def test_token_option_only_accepts_environment_names(token_env, tmp_path):
    with pytest.raises(PreparationError, match="^invalid_model_source$"):
        acquire(SOURCE, tmp_path / "new", token_env)


@pytest.mark.parametrize("value", ["", "has space"])
def test_blank_or_unsafe_token_values_are_rejected(tmp_path, monkeypatch, value):
    monkeypatch.setenv("HF_TOKEN", value)
    with pytest.raises(PreparationError, match="^model_acquire_incomplete$"):
        acquire(SOURCE, tmp_path / "new", "HF_TOKEN")


@pytest.mark.parametrize("seconds", [0, -1, True, float("nan"), float("inf"), "1800"])
def test_timeout_must_be_a_positive_finite_number(tmp_path, seconds):
    with pytest.raises(PreparationError, match="^invalid_model_source$"):
        acquire(SOURCE, tmp_path / "new", timeout_seconds=seconds)
