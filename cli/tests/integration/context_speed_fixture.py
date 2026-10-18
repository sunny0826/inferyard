"""Synthetic TCP Prism service and explicitly injected host/review evidence.

This fixture does not run a model or approve the production corpus.
"""

import asyncio
import hashlib
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

from inferyard.adapters.prism import BUILD, PrismAdapter
from inferyard.config.context_speed_profile import GENERATION
from inferyard.config.toml_writer import render
from inferyard.platforms.identity import FileIdentity
from tests.integration.test_runner import scenario as runner_scenario

scenario = runner_scenario


@pytest.fixture
def speed_service(scenario, tmp_path, monkeypatch):
    command, deps, _, _ = scenario
    config = command.config.config.to_dict()
    state = {
        "requests": [],
        "calls": [],
        "params": {},
        "formal_tokens": [100] * 5,
        "timing": True,
        "formal_output": 256,
        "formal_delay": 0,
    }

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def respond(self, value, *, stream=False, extra_usage=False):
            frames = [value]
            if extra_usage:
                frames.append({"choices": [], "usage": value["usage"]})
            raw = (
                (
                    "".join("data: " + json.dumps(f) + "\n\n" for f in frames) + "data: [DONE]\n\n"
                ).encode()
                if stream
                else json.dumps(value).encode()
            )
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream" if stream else "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            try:
                self.wfile.write(raw)
            except BrokenPipeError, ConnectionResetError:
                pass

        def do_GET(self):
            state["calls"].append(self.path)
            if self.path == "/slots":
                return self.respond(
                    [
                        {
                            "id": 0,
                            "is_processing": False,
                            "params": state["params"],
                            "n_ctx": config["conditions"]["context_size"],
                        }
                    ]
                )
            assert self.path == "/props"
            self.respond(
                {
                    "build_info": BUILD,
                    "total_slots": 1,
                    "default_generation_settings": {"n_ctx": config["conditions"]["context_size"]},
                    "model_path": config["model"]["local_path"],
                    "chat_template": "fixture-template",
                }
            )

        def do_POST(self):
            state["calls"].append(self.path)
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            if self.path == "/apply-template":
                formal = len(body["messages"][0]["content"]) > 1000
                return self.respond({"prompt": "formal-rendered" if formal else "probe-rendered"})
            if self.path == "/tokenize":
                index = min(len(state["requests"]) // 4, 4)
                tokens = (
                    state["formal_tokens"][index] if body["content"] == "formal-rendered" else 1
                )
                return self.respond({"tokens": list(range(tokens))})
            assert self.path == "/v1/chat/completions"
            formal = len(body["messages"][0]["content"]) > 1000
            index = min(len(state["requests"]) // 4, 4)
            state["requests"].append(body)
            state["params"].update(body)
            if formal and state["formal_delay"]:
                import time

                time.sleep(state["formal_delay"])
            count = state["formal_tokens"][index] if formal else 1
            output = state["formal_output"] if formal else 256
            choice = {"index": 0, "finish_reason": "length"}
            choice["delta" if body["stream"] else "message"] = {
                "content": "fixture",
                **({} if body["stream"] else {"role": "assistant"}),
            }
            response = {
                "choices": [choice],
                "usage": {
                    "prompt_tokens": state.get("formal_usage_prompt", count) if formal else count,
                    "completion_tokens": output,
                },
            }
            if state["timing"]:
                response["timings"] = state.get("custom_timing") or {
                    "cache_n": 0,
                    "prompt_n": count,
                    "prompt_ms": 400,
                    "predicted_n": output,
                    "predicted_ms": 500,
                }
            self.respond(
                response,
                stream=body["stream"],
                extra_usage=formal and state.get("duplicate_usage", False),
            )

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    origin = f"http://127.0.0.1:{server.server_port}"
    config["endpoint"]["url"] = origin
    config["bundle"] = {"path": str(tmp_path / "bundle.json"), "version": "2026.10.1"}
    config["conditions"]["context_size"] = 65536
    config["generation"] = dict(GENERATION)
    config["execution"].update(warmup_count=1, timeout_seconds=3)
    config["engine"]["startup_args"] += [
        "-m",
        config["model"]["local_path"],
        "-c",
        "65536",
        "-np",
        "1",
    ]
    environment = {
        "platform": "Linux",
        "architecture": "fixture",
        "kernel": "fixture",
        "cpu_model": "synthetic",
        "logical_cpus": 1,
        "memory_total_bytes": 16 * 1024**3,
        "gpu": {"backend": "not_used", "reason": "cpu_only_scope"},
    }
    files = [
        FileIdentity(config[s][p], config[s][h], 1, 1, 1, 1)
        for s, p, h in (
            ("model", "local_path", "sha256"),
            ("model", "template_path", "template_sha256"),
            ("engine", "binary_path", "binary_sha256"),
        )
    ]
    deps.preflight = lambda value: (
        {"origin": origin, "environment": environment, "verification": "verified"},
        files,
    )
    deps.adapter = PrismAdapter
    bundle = Path(__file__).resolve().parents[2] / "bundles/zh-context-speed.json"
    (tmp_path / "bundle.json").write_bytes(bundle.read_bytes())
    path = tmp_path / "speed.toml"

    def write(context=65536):
        config["conditions"]["context_size"] = context
        config["engine"]["startup_args"][-3] = str(context)
        path.write_text(render(config), encoding="utf-8")
        return path

    import inferyard.runtime.batch_runner as batch_runner

    original = batch_runner.execute_async
    monkeypatch.setattr(
        batch_runner, "execute", lambda request: asyncio.run(original(request, deps))
    )
    state.update(config=config, write=write, deps=deps, root=tmp_path)
    try:
        yield state
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=2)


def allow_synthetic_review(monkeypatch):
    monkeypatch.setattr("inferyard.config.context_speed_plan.require_review", lambda bundle: None)
    monkeypatch.setattr("inferyard.runtime.trial_runner.require_review", lambda bundle: None)


def tree_hashes(root):
    return {
        p.relative_to(root).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in root.rglob("*")
        if p.is_file()
    }
