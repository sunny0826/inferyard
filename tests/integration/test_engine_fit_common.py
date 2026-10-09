"""Real TCP protocol with synthetic Darwin identity/resources and LMS observer.

These tests never load a model, execute lms, sample SMC, or prove native engine fit.
"""

import json
import shutil
import struct
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import read_json


@pytest.fixture
def common_fit_service(tmp_path, monkeypatch):
    from inferyard.platforms import engine_fit as identity
    from inferyard.platforms import engine_fit_lmstudio, sensors_macos
    from inferyard.runtime import engine_fit as runtime
    from inferyard.runtime import lock

    host = {"sha256": "a" * 64, "platform": "Darwin", "architecture": "synthetic-arm64"}
    monkeypatch.setattr(identity, "host_identity", lambda: host)
    monkeypatch.setattr(runtime, "host_identity", lambda: host)
    monkeypatch.setattr(runtime.platform, "system", lambda: "Darwin")
    monkeypatch.setattr(lock, "LOCK_PATH", tmp_path / "host.lock")
    monkeypatch.setattr(lock, "STATE_PATH", tmp_path / "host.state.json")
    from tests.host_state_helpers import initialize

    initialize()
    monkeypatch.setattr(lock, "process_start_ticks", lambda _pid: 100)

    class EmptySensors:
        sources = []

        def collect(self, _phase, _request):
            return []

    monkeypatch.setattr(sensors_macos, "MacSensors", EmptySensors)
    models_root = tmp_path / "models"
    models_root.mkdir()
    model = models_root / "synthetic.gguf"
    # Only a bounded metadata header; deliberately not executable model weights.
    model.write_bytes(b"GGUF" + struct.pack("<IQQ", 3, 1, 0) + b"synthetic tensor bytes")
    lms_path = tmp_path / "synthetic-lms"
    lms_path.write_text("Never executed: observer is simulated in this test.")
    assert (
        main(
            [
                "engine-fit",
                "plan",
                "--model",
                str(model),
                "--engines",
                "llama-cpp",
                "lmstudio",
                "--out",
                str(tmp_path / "plan"),
            ]
        )
        == 0
    )
    state = {
        "engine": "llama-cpp",
        "posts": [],
        "gets": [],
        "broken": False,
        "observer_inspections": [],
        "observer_idle": [],
        "bindings": [],
        "closed": False,
    }

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_args):
            pass

        def do_GET(self):
            state["gets"].append((state["engine"], self.path))
            if self.path == "/v1/models":
                data = [{"id": "synthetic"}]
                if state["engine"] == "lmstudio":
                    # LM Studio lists cached candidates as well as loaded instances.
                    data.append({"id": "cached-but-not-loaded"})
                raw = json.dumps({"object": "list", "data": data}).encode()
                mime = "application/json"
            elif self.path == "/props" and state["engine"] == "llama-cpp":
                raw = json.dumps({"build_info": "b11146-7fe450e19", "private": "omit_me"}).encode()
                mime = "application/json"
            elif self.path == "/metrics" and state["engine"] == "llama-cpp":
                raw = b"llamacpp:requests_processing 0\nllamacpp:requests_deferred 0\n"
                mime = "text/plain; version=0.0.4"
            else:
                self.send_error(404)
                return
            self.send_response(200)
            self.send_header("Content-Type", mime)
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_POST(self):
            if self.path != "/v1/chat/completions":
                self.send_error(404)
                return
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            state["posts"].append((state["engine"], body))
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            chunks = [
                {
                    "choices": [
                        {
                            "index": 0,
                            "delta": {"content": "42 <script>synthetic</script>"},
                            "finish_reason": None,
                        }
                    ]
                },
                {"choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}]},
                {
                    "choices": [],
                    "usage": {"prompt_tokens": 9, "completion_tokens": 3, "total_tokens": 12},
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

    def bind(engine, model_path, pid, url, **options):
        state["bindings"].append((engine, options))
        metadata = model_path.stat()
        binding = {
            "pid": pid,
            "start_ticks": 100,
            "origin": url,
            "address": "127.0.0.1",
            "port": server.server_port,
            "executable_sha256": "b" * 64,
            "argv_sha256": "c" * 64,
            "cwd_sha256": "d" * 64,
            "listener_inode": None,
            "listener_identity": f"macos:tcp:127.0.0.1:{server.server_port}:pid:{pid}",
            "listener_source": "lsof:TCP:LISTEN:pid",
            "model_binding": {
                "engine": engine,
                "path": str(model_path),
                "device": metadata.st_dev,
                "inode": metadata.st_ino,
                "source": "lms_loaded_instance_path"
                if engine == "lmstudio"
                else "verified_startup_file_identity",
            },
        }
        if engine == "lmstudio":
            assert options == {
                "lms_path": lms_path,
                "models_root": models_root,
                "served_model": "synthetic",
            }
            binding["observer"] = {
                "kind": "lms",
                "path": str(lms_path),
                "sha256": "e" * 64,
                "models_root": str(models_root),
                "instance_id": "synthetic",
            }
        else:
            assert options == {}
        return binding

    class SyntheticLMStudioObserver:
        def __init__(self, binding):
            self.binding = binding

        async def inspect(self):
            state["observer_inspections"].append(self.binding)
            return {
                "engine": "lmstudio",
                "version": None,
                "served_model": "synthetic",
                "version_source": "not_exposed",
                "version_missing_reason": "lmstudio_service_version_not_exposed",
            }

        async def idle(self):
            state["observer_idle"].append(self.binding)
            return {
                "idle": True,
                "source": "lms:ps",
                "values": [
                    {"metric": metric, "labels": {"instance": "synthetic"}, "value": 0}
                    for metric in ("lmstudio:active", "lmstudio:queued")
                ],
            }

    monkeypatch.setattr(engine_fit_lmstudio, "LMStudioObserver", SyntheticLMStudioObserver)
    monkeypatch.setattr(runtime, "bind_service", bind)
    monkeypatch.setattr(runtime, "check_service", lambda _binding: None)
    monkeypatch.setattr(
        runtime,
        "resource_snapshot",
        lambda _binding: {
            "memory_available_bytes": 8 * 2**30,
            "process_tree_rss_bytes": 2**20,
            "process_tree_cpu_seconds": 1.0,
            "process_count": 1,
            "scope": {"source": "synthetic_darwin_resources_no_smc_or_real_process"},
            "missing_reasons": {},
        },
    )

    def close():
        if not state["closed"]:
            state["closed"] = True
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)

    yield {
        "root": tmp_path,
        "state": state,
        "origin": origin,
        "model": model,
        "lms_path": lms_path,
        "models_root": models_root,
        "close": close,
        "runtime": runtime,
    }
    close()


def run(fixture, engine, name):
    fixture["state"]["engine"] = engine
    arguments = [
        "engine-fit",
        "run",
        "--plan",
        str(fixture["root"] / "plan/plan.json"),
        "--engine",
        engine,
        "--endpoint-url",
        fixture["origin"],
        "--server-pid",
        "42",
        "--served-model",
        "synthetic",
        "--out",
        str(fixture["root"] / name),
    ]
    if engine == "lmstudio":
        arguments.extend(
            ["--lms-path", str(fixture["lms_path"]), "--models-root", str(fixture["models_root"])]
        )
    return main(arguments)


def test_gguf_pair_full_run_compare_then_offline_verify(common_fit_service, monkeypatch, capsys):
    fixture = common_fit_service
    root, state = fixture["root"], fixture["state"]
    plan = read_json(root / "plan/plan.json")
    assert plan["definition"] == "engine_fit_plan.v2" and plan["model"]["kind"] == "gguf"
    for engine in ("llama-cpp", "lmstudio"):
        assert run(fixture, engine, engine) == 0, capsys.readouterr()
        evidence = read_json(root / engine / "run.json")
        assert evidence["definition"] == "engine_fit_run.v3"
        assert evidence["platform"] == "Darwin"
        assert evidence["counts"] == {
            "planned": 3,
            "completed": 3,
            "failed": 0,
            "cancelled": 0,
            "invalid": 0,
            "not_executed": 0,
        }
        assert evidence["diagnostic"] is True
        assert evidence["performance_comparison_qualified"] is False
        assert all(sample["temperature_samples"] == [] for sample in evidence["resources"])
    service = read_json(root / "lmstudio/run.json")["service"]
    assert service["version"] is None
    assert service["version_missing_reason"] == "lmstudio_service_version_not_exposed"
    assert service["idle_before"]["source"] == "lms:ps"
    assert state["observer_inspections"] and len(state["observer_idle"]) == 7
    assert state["observer_inspections"][0]["origin"] == fixture["origin"]
    assert ("lmstudio", "/metrics") not in state["gets"]
    assert len(state["posts"]) == 6
    assert [body for _engine, body in state["posts"][:3]] == [
        body for _engine, body in state["posts"][3:]
    ]
    assert not read_json(root / "host.state.json")["dirty"]
    assert (
        main(
            [
                "engine-fit",
                "compare",
                "--runs",
                str(root / "llama-cpp"),
                str(root / "lmstudio"),
                "--out",
                str(root / "comparison"),
            ]
        )
        == 0
    ), capsys.readouterr()
    fixture["close"]()
    shutil.rmtree(root / "llama-cpp")
    shutil.rmtree(root / "lmstudio")
    fixture["model"].unlink()

    def forbidden_live_access(*_args, **_kwargs):
        pytest.fail("offline verification attempted live service access")

    monkeypatch.setattr(fixture["runtime"], "FitClient", forbidden_live_access)
    monkeypatch.setattr(fixture["runtime"], "host_identity", forbidden_live_access)
    assert main(["engine-fit", "verify", "--path", str(root / "comparison")]) == 0
    html = (root / "comparison/report.html").read_text()
    assert "llama-cpp" in html and "lmstudio" in html
    assert "<script>synthetic</script>" not in html
    assert "omit_me" not in (root / "comparison/comparison.json").read_text()


@pytest.mark.parametrize("engine", ["llama-cpp", "lmstudio"])
def test_common_engine_disconnect_seals_incomplete_and_preserves_dirty(
    common_fit_service, engine, capsys
):
    fixture = common_fit_service
    root, state = fixture["root"], fixture["state"]
    state["broken"] = True
    assert run(fixture, engine, "broken") == 3, capsys.readouterr()
    assert len(state["posts"]) == 1
    dirty = read_json(root / "host.state.json")
    assert dirty["dirty"] and dirty["dirty_token"]
    evidence = read_json(root / "broken/run.json")
    assert evidence["completeness"] == "incomplete"
    assert evidence["counts"] == {
        "planned": 3,
        "completed": 0,
        "failed": 1,
        "cancelled": 0,
        "invalid": 0,
        "not_executed": 2,
    }
    assert main(["engine-fit", "verify", "--path", str(root / "broken")]) == 0
    state["broken"] = False
    assert run(fixture, engine, "no-implicit-recovery") == 2
    assert len(state["posts"]) == 1
    assert read_json(root / "host.state.json")["dirty_token"] == dirty["dirty_token"]


def test_changed_gguf_is_blocked_before_any_service_access(common_fit_service, capsys):
    fixture = common_fit_service
    fixture["model"].write_bytes(fixture["model"].read_bytes() + b"changed")
    assert run(fixture, "llama-cpp", "changed") == 2
    assert "engine_fit_model_changed" in capsys.readouterr().out
    assert not fixture["state"]["posts"] and not fixture["state"]["gets"]
    assert not fixture["state"]["bindings"]
    assert not (fixture["root"] / "changed").exists()


def test_existing_plan_run_and_comparison_never_overwritten(common_fit_service, capsys):
    fixture = common_fit_service
    root, state = fixture["root"], fixture["state"]
    frozen = (root / "plan/plan.json").read_bytes()
    assert (
        main(["engine-fit", "plan", "--model", str(fixture["model"]), "--out", str(root / "plan")])
        == 2
    )
    assert (root / "plan/plan.json").read_bytes() == frozen
    for engine in ("llama-cpp", "lmstudio"):
        assert run(fixture, engine, engine) == 0, capsys.readouterr()
    before = {path.name: path.read_bytes() for path in (root / "llama-cpp").iterdir()}
    assert run(fixture, "llama-cpp", "llama-cpp") == 2
    assert {path.name: path.read_bytes() for path in (root / "llama-cpp").iterdir()} == before
    arguments = [
        "engine-fit",
        "compare",
        "--runs",
        str(root / "llama-cpp"),
        str(root / "lmstudio"),
        "--out",
        str(root / "comparison"),
    ]
    assert main(arguments) == 0, capsys.readouterr()
    comparison = (root / "comparison/manifest.json").read_bytes()
    assert main(arguments) == 4
    assert (root / "comparison/manifest.json").read_bytes() == comparison
    assert len(state["posts"]) == 6
