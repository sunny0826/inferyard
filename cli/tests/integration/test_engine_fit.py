"""Real loopback HTTP with synthetic process identity; never a real model benchmark."""

import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from inferyard.application.types import CommandRequest
from inferyard.cli import main
from inferyard.config.engine_fit import prepare
from inferyard.evidence.storage import read_json


@pytest.fixture
def fit_service(tmp_path, monkeypatch):
    from inferyard.platforms import engine_fit as identity
    from inferyard.runtime import engine_fit as runtime
    from inferyard.runtime import lock

    host = {"sha256": "a" * 64, "platform": "Linux", "architecture": "fixture"}
    monkeypatch.setattr(identity, "host_identity", lambda: host)
    monkeypatch.setattr(runtime, "host_identity", lambda: host)
    monkeypatch.setattr(runtime.platform, "system", lambda: "Linux")
    monkeypatch.setattr(lock, "LOCK_PATH", tmp_path / "host.lock")
    monkeypatch.setattr(lock, "STATE_PATH", tmp_path / "host.state.json")
    from tests.host_state_helpers import initialize

    initialize()
    model = tmp_path / "model"
    model.mkdir()
    (model / "config.json").write_text('{"model_type":"fixture"}')
    (model / "model.safetensors").write_text("not real weights")
    prepare(CommandRequest("engine-fit plan", model_path=model, out=tmp_path / "plan"))
    state = {"engine": "vllm", "posts": [], "broken": False, "secret": None}

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            if self.path == "/metrics":
                engine = state["engine"]
                names = (
                    ("num_requests_running", "num_requests_waiting")
                    if engine == "vllm"
                    else ("num_running_reqs", "num_queue_reqs")
                )
                raw = "\n".join(f'{engine}:{n}{{model_name="fixture"}} 0' for n in names) + "\n"
                self.send_response(200)
                self.end_headers()
                self.wfile.write(raw.encode())
                return
            payload = (
                {"data": [{"id": "fixture"}]}
                if self.path == "/v1/models"
                else {"version": "0.0.1+fixture"}
            )
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(json.dumps(payload).encode())

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            state["posts"].append(body)
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            chunks = [
                {"choices": [{"index": 0, "delta": {"role": "assistant"}, "finish_reason": None}]},
                {
                    "choices": [
                        {
                            "index": 0,
                            "delta": {"content": state["secret"] or "42 <script>x</script>"},
                            "finish_reason": None,
                        }
                    ]
                },
                {"choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]},
                {
                    "choices": [],
                    "usage": {"prompt_tokens": 12, "completion_tokens": 3, "total_tokens": 15},
                },
            ]
            for chunk in chunks:
                self.wfile.write(("data: " + json.dumps(chunk) + "\n\n").encode())
            if not state["broken"]:
                self.wfile.write(b"data: [DONE]\n\n")

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    origin = f"http://127.0.0.1:{server.server_port}"

    def bind(engine, model_path, pid, url):
        return {
            "pid": pid,
            "start_ticks": 100,
            "origin": url,
            "address": "127.0.0.1",
            "port": server.server_port,
            "executable_sha256": "b" * 64,
            "argv_sha256": "c" * 64,
            "listener_inode": "123",
            "model_binding": {
                "source": "verified_startup_directory_identity",
                "path": str(model_path),
                "engine": engine,
                "device": 1,
                "inode": 2,
            },
        }

    monkeypatch.setattr(runtime, "bind_service", bind)
    monkeypatch.setattr(runtime, "check_service", lambda binding: None)
    monkeypatch.setattr(
        runtime,
        "resource_snapshot",
        lambda binding: {
            "memory_available_bytes": 8 * 2**30,
            "process_tree_rss_bytes": 2**20,
            "process_tree_cpu_seconds": 1.0,
            "process_count": 2,
            "scope": {"source": "synthetic_proc", "rss": "tree_sum_including_shared_pages"},
            "missing_reasons": {},
        },
    )
    monkeypatch.setattr(lock, "process_start_ticks", lambda pid: 100)
    yield tmp_path, state, origin, runtime
    server.shutdown()
    server.server_close()
    thread.join()


def run_cli(root, origin, engine, out, extras=()):
    return main(
        [
            "engine-fit",
            "run",
            "--plan",
            str(root / "plan/plan.json"),
            "--engine",
            engine,
            "--endpoint-url",
            origin,
            "--server-pid",
            "42",
            "--served-model",
            "fixture",
            "--out",
            str(out),
            *extras,
        ]
    )


def test_changed_model_blocks_before_requests(fit_service, capsys):
    root, state, origin, _ = fit_service
    (root / "model/model.safetensors").write_text("changed")
    assert run_cli(root, origin, "vllm", root / "changed") == 2
    assert state["posts"] == []
    assert "engine_fit_model_changed" in capsys.readouterr().out


def test_manual_recovery_requires_old_process_gone(fit_service, monkeypatch, capsys):
    from inferyard.runtime import lock

    root, state, origin, runtime = fit_service
    state["broken"] = True
    assert run_cli(root, origin, "vllm", root / "dirty") == 3, capsys.readouterr()
    token = read_json(root / "host.state.json")["dirty_token"]
    state["broken"] = False
    options = ["--recovery-confirm", token, "--recovery-note", "operator restarted service"]
    assert run_cli(root, origin, "vllm", root / "still-alive", options) == 2
    assert len(state["posts"]) == 1
    original = runtime.bind_service
    monkeypatch.setattr(
        runtime, "bind_service", lambda *args: {**original(*args), "start_ticks": 101}
    )
    monkeypatch.setattr(lock, "process_start_ticks", lambda pid: 101)
    assert run_cli(root, origin, "vllm", root / "recovered", options) == 0, capsys.readouterr()
    assert len(state["posts"]) == 4
    assert not read_json(root / "host.state.json")["dirty"]
