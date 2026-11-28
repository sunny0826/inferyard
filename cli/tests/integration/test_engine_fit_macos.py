"""Native Darwin lifecycle and evidence, with synthetic HTTP engines and model bytes."""

import json
import os
import selectors
import subprocess
import sys
from contextlib import contextmanager
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.errors import PreflightError
from inferyard.platforms.engine_fit import bind_service, check_service, resource_snapshot

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
        module = {"vllm": "vllm.entrypoints.openai.api_server"}[engine]
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
                "--model",
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


def _posts(record):
    return [json.loads(line) for line in record.read_text().splitlines()] if record.exists() else []


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
