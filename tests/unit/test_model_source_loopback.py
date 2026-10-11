"""Real HTTP body timeout/trickle simulations, bound exclusively to IPv4 loopback."""

import hashlib
import json
import subprocess
import sys
import threading
import time
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

from inferyard.config.preparation_io import PreparationError
from inferyard.platforms import model_source_host as host
from tests.unit.test_model_acquire_cli import SOURCE
from tests.unit.test_model_source import FIXTURES, source
from tests.unit.test_model_source_download import PAYLOAD


@contextmanager
def body_server(mode):
    metadata = json.loads((FIXTURES / "huggingface.json").read_bytes())
    selected = next(f for f in metadata["siblings"] if f["rfilename"] == source("huggingface").path)
    selected["size"] = len(PAYLOAD)
    selected["lfs"].update(size=len(PAYLOAD), sha256=hashlib.sha256(PAYLOAD).hexdigest())
    stopped = threading.Event()
    paths = []

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            paths.append(self.path)
            is_metadata = self.path.startswith("/api/")
            if mode == "redirect" and "/resolve/" in self.path:
                self.send_response(302)
                self.send_header("Location", "https://cdn.hf.co/fixture")
                self.send_header("Content-Length", "5000")
                self.end_headers()
                stopped.wait(5)
                return
            raw = json.dumps(metadata).encode() if is_metadata else PAYLOAD
            self.send_response(200)
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            try:
                if is_metadata:
                    self.wfile.write(raw)
                    self.wfile.flush()
                elif mode == "stall":
                    stopped.wait(5)
                else:
                    for byte in raw:
                        self.wfile.write(bytes([byte]))
                        self.wfile.flush()
                        if stopped.wait(0.06):
                            return
            except OSError:
                pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.02})
    thread.start()
    try:
        yield server.server_port, paths
    finally:
        stopped.set()
        server.shutdown()
        server.server_close()
        thread.join(timeout=1)
        assert not thread.is_alive()


def loopback_worker(monkeypatch, port):
    original = subprocess.Popen
    children = []
    fixture = Path(__file__).parents[1] / "fixtures/model_source_loopback_worker.py"

    def create(args, **kwargs):
        child = original([sys.executable, str(fixture), str(port)], **kwargs)
        children.append(child)
        return child

    monkeypatch.setattr(host.subprocess, "Popen", create)
    return children


def test_stalled_body_terminates_with_read_diagnostic(tmp_path, monkeypatch):
    with body_server("stall") as (port, paths):
        children = loopback_worker(monkeypatch, port)
        started = time.monotonic()
        with pytest.raises(PreparationError) as caught:
            host.acquire(
                SOURCE,
                tmp_path / "new",
                budget=host.Budget(total_seconds=5, read_seconds=0.6, cleanup_seconds=0.5),
            )
        assert time.monotonic() - started < 3
        assert len(paths) == 2
        assert caught.value.diagnostics["stage"] == "download-body"
        assert caught.value.diagnostics["exception_class"] == "read_timeout"
        assert caught.value.diagnostics["bytes_written"] == 0
        assert children[0].poll() is not None
        assert not (tmp_path / "new/source.json").exists()


@pytest.mark.parametrize("mode", ["trickle", "redirect"])
def test_small_raw_chunks_keep_transfer_alive_and_publish_hash(tmp_path, monkeypatch, mode):
    with body_server(mode) as (port, paths):
        children = loopback_worker(monkeypatch, port)
        details = host.acquire(
            SOURCE,
            tmp_path / "new",
            budget=host.Budget(total_seconds=5, read_seconds=0.6, cleanup_seconds=0.5),
        )
        assert Path(details["model"]).read_bytes() == PAYLOAD
        assert details["sha256"] == hashlib.sha256(PAYLOAD).hexdigest()
        assert len(paths) == (3 if mode == "redirect" else 2)
        assert children[0].poll() is not None


def test_total_timeout_reports_partial_sub_megabyte_progress(tmp_path, monkeypatch):
    with body_server("trickle") as (port, _):
        loopback_worker(monkeypatch, port)
        with pytest.raises(PreparationError) as caught:
            host.acquire(
                SOURCE,
                tmp_path / "new",
                budget=host.Budget(total_seconds=1.2, read_seconds=0.6, cleanup_seconds=0.5),
            )
        detail = caught.value.diagnostics
        assert detail["exception_class"] == "total_timeout"
        assert 0 < detail["bytes_written"] < len(PAYLOAD)
        assert detail["elapsed_seconds"] >= 1.2
        assert detail["average_bytes_per_second"] > 0
        assert not (tmp_path / "new/source.json").exists()
