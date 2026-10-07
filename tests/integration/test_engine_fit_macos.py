"""Native Darwin lifecycle and evidence, with synthetic HTTP engines and model bytes."""

import json
import os
import selectors
import shutil
import subprocess
import sys
from contextlib import contextmanager
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import read_json
from inferyard.platforms.engine_fit import bind_service, check_service, resource_snapshot
from inferyard.platforms.identity import PreflightError

pytestmark = pytest.mark.skipif(sys.platform != "darwin", reason="Native Darwin engine-fit test")
SERVICE = Path(__file__).parents[1] / "fixtures/engine_fit_macos/service.py"


def _readline(process):
    with selectors.DefaultSelector() as ready:
        ready.register(process.stdout, selectors.EVENT_READ)
        assert ready.select(5), "native HTTP fixture did not respond within its startup budget"
    line = process.stdout.readline()
    assert line, "native HTTP fixture exited (check local socket permission)"
    return line.strip()


@pytest.fixture
def native_fit(tmp_path, monkeypatch, capsys):
    from inferyard.runtime import lock

    monkeypatch.setattr(lock, "LEGACY_ROOT", None)
    monkeypatch.setattr(lock, "LOCK_PATH", tmp_path / "host.lock")
    monkeypatch.setattr(lock, "STATE_PATH", tmp_path / "host.state.json")
    from tests.host_state_helpers import initialize

    initialize()
    model = tmp_path / "model"
    model.mkdir()
    (model / "config.json").write_text('{"model_type":"synthetic-fixture"}', encoding="utf-8")
    (model / "model.safetensors").write_bytes(b"synthetic bytes; not model weights")
    assert (
        main(
            [
                "engine-fit",
                "plan",
                "--model",
                str(model),
                "--max-tokens",
                "8",
                "--request-timeout",
                "3",
                "--out",
                str(tmp_path / "plan"),
            ]
        )
        == 0
    ), capsys.readouterr()
    started = 0

    @contextmanager
    def start(engine, startup_model=None, *, port=0, reuse_port=False):
        nonlocal started
        started += 1
        root = tmp_path / f"service-{started}"
        module = {
            "vllm": "vllm.entrypoints.openai.api_server",
            "sglang": "sglang.launch_server",
        }[engine]
        directory = root
        for part in module.split(".")[:-1]:
            directory /= part
            directory.mkdir(parents=True)
            (directory / "__init__.py").write_text("", encoding="utf-8")
        (directory / (module.split(".")[-1] + ".py")).write_text(
            f"from runpy import run_path\nrun_path({str(SERVICE)!r}, run_name='__main__')\n",
            encoding="utf-8",
        )
        record = root / "requests.jsonl"
        process = subprocess.Popen(
            [
                sys.executable,
                "-u",
                "-m",
                module,
                "--model" if engine == "vllm" else "--model-path",
                str(startup_model or model),
                "--engine",
                engine,
                "--record",
                str(record),
                "--port",
                str(port),
                *(["--reuse-port"] if reuse_port else []),
            ],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
            cwd=root,
            env={**os.environ, "PYTHONPATH": str(root)},
        )
        try:
            ready = json.loads(_readline(process))
            yield process, f"http://127.0.0.1:{ready['port']}", record, ready["child_pid"]
        finally:
            if process.poll() is None:
                try:
                    _, stderr = process.communicate("stop\n", timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    _, stderr = process.communicate(timeout=5)
                    pytest.fail(f"native HTTP fixture failed to stop: {stderr}")
            else:
                _, stderr = process.communicate(timeout=5)
            assert process.returncode == 0, stderr

    return tmp_path, model, start


def _run(root, service, engine, name):
    process, origin, *_ = service
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
            str(process.pid),
            "--served-model",
            "native-macos-fixture",
            "--out",
            str(root / name),
        ]
    )


def _posts(record):
    return [json.loads(line) for line in record.read_text().splitlines()] if record.exists() else []


def _stop_on_temperature(root, engine, record, code, output):
    """A native hardware stop is retained as incomplete evidence, never retried."""
    payload = json.loads(output.out.strip().splitlines()[-1])
    reason = "temperature_safety_threshold_reached"
    if (
        reason not in payload.get("limitations", [])
        and (payload.get("details") or {}).get("stop_reason") != reason
    ):
        return
    directory = root / engine
    posts = _posts(record)
    if code == 2:
        assert not directory.exists()
        assert posts == []
        pytest.skip("hardware threshold before evidence creation; native fit smoke not validated")
    assert code == 3
    run = read_json(directory / "run.json")
    rows = read_json(directory / "requests.json")
    assert run["stop_reason"] == reason and run["completeness"] == "incomplete"
    assert run["counts"]["planned"] == len(rows) == 3
    assert sum(value for key, value in run["counts"].items() if key != "planned") == 3
    for state in ("completed", "failed", "cancelled", "invalid", "not_executed"):
        assert run["counts"][state] == sum(row["status"] == state for row in rows)
    assert len(posts) <= 3 - run["counts"]["not_executed"]
    assert all(row["status"] != "not_executed" for row in rows[: len(posts)])
    assert all(row["response"] is None for row in rows if row["status"] == "not_executed")
    assert any(
        sample["phase"].startswith("stop:")
        and any(
            reading["value"] is not None and reading["value"] >= 85
            for reading in sample["temperature_samples"]
        )
        for sample in run["resources"]
    )
    assert main(["engine-fit", "verify", "--path", str(directory)]) == 0
    pytest.skip(f"hardware threshold; native fit smoke not validated; evidence: {directory}")


@pytest.mark.skipif(
    os.environ.get("LAB_BENCH_NATIVE_FIT_SMOKE") != "1",
    reason="Opt-in native SMC/request smoke: LAB_BENCH_NATIVE_FIT_SMOKE=1",
)
def test_native_mac_both_engines_run_compare_and_offline_verify(native_fit, capsys):
    root, _, start = native_fit
    frozen = read_json(root / "plan/plan.json")
    assert frozen["host"]["platform"] == "Darwin"
    sent = []
    for engine in ("vllm", "sglang"):
        with start(engine) as service:
            process, _, record, child_pid = service
            code = _run(root, service, engine, engine)
            output = capsys.readouterr()
            _stop_on_temperature(root, engine, record, code, output)
            assert code == 0, output
            assert process.poll() is None
            assert child_pid != process.pid
            run = read_json(root / engine / "run.json")
            assert run["definition"] == "engine_fit_run.v2"
            assert run["binding"]["pid"] == process.pid
            assert run["binding"]["start_ticks"] > 0
            assert run["binding"]["listener_inode"] is None
            assert run["binding"]["listener_source"] == "lsof:TCP:LISTEN:pid"
            assert run["diagnostic"] is True
            assert run["performance_comparison_qualified"] is False
            assert run["counts"]["completed"] == 3
            for sample in run["resources"]:
                assert sample["process_count"] == 2
                assert sample["process_tree_rss_bytes"] > 8 * 2**20
                assert sample["process_tree_cpu_seconds"] >= 0
                assert sample["memory_available_bytes"] > 0
                assert sample["missing_reasons"] == {}
            assert main(["engine-fit", "verify", "--path", str(root / engine)]) == 0
            sent.append(_posts(record))
    assert len(sent[0]) == len(sent[1]) == 3
    assert sent[0] == sent[1]
    assert not read_json(root / "host.state.json")["dirty"]
    assert (
        main(
            [
                "engine-fit",
                "compare",
                "--runs",
                str(root / "vllm"),
                str(root / "sglang"),
                "--out",
                str(root / "comparison"),
            ]
        )
        == 0
    ), capsys.readouterr()
    shutil.rmtree(root / "vllm")
    shutil.rmtree(root / "sglang")
    assert main(["engine-fit", "verify", "--path", str(root / "comparison")]) == 0
    html = (root / "comparison/report.html").read_text()
    assert "<script>x</script>" not in html
    assert "vllm" in html and "sglang" in html


def test_native_wrong_listener_pid_blocks_without_request(native_fit, capsys):
    root, _, start = native_fit
    with start("vllm") as first, start("vllm") as second:
        wrong = (second[0], first[1], *first[2:])
        assert _run(root, wrong, "vllm", "wrong-pid") == 2, capsys.readouterr()
        assert "endpoint_pid_mismatch" in capsys.readouterr().out
        assert _posts(first[2]) == _posts(second[2]) == []
        assert not (root / "wrong-pid").exists()


def test_native_wrong_startup_model_blocks_without_request(native_fit, capsys):
    root, _, start = native_fit
    other = root / "different-model"
    other.mkdir()
    with start("sglang", other) as service:
        assert _run(root, service, "sglang", "wrong-model") == 2, capsys.readouterr()
        assert "engine_fit_model_argument_mismatch" in capsys.readouterr().out
        assert _posts(service[2]) == []
        assert not (root / "wrong-model").exists()


def test_native_closed_listener_and_reaped_service_invalidate_binding(native_fit):
    _, model, start = native_fit
    with start("vllm") as (process, origin, record, _):
        binding = bind_service("vllm", model, process.pid, origin)
        assert resource_snapshot(binding)["process_count"] == 2
        process.stdin.write("close\n")
        process.stdin.flush()
        assert _readline(process) == "closed"
        assert process.poll() is None
        with pytest.raises(PreflightError, match="endpoint_pid_mismatch"):
            check_service(binding)
        assert _posts(record) == []
    with pytest.raises(PreflightError, match="service_process_unavailable"):
        check_service(binding)


def test_native_reused_port_with_multiple_owners_is_rejected(native_fit):
    _, model, start = native_fit
    with start("vllm", reuse_port=True) as first:
        port = int(first[1].rpartition(":")[2])
        with start("vllm", port=port, reuse_port=True) as second:
            assert first[0].pid != second[0].pid and first[1] == second[1]
            for process, origin, record, _ in (first, second):
                with pytest.raises(PreflightError, match="listener_ambiguous"):
                    bind_service("vllm", model, process.pid, origin)
                assert _posts(record) == []
