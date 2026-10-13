"""Native Darwin TCP child identity only: synthetic model bytes, no inference."""

import json
import os
import selectors
import subprocess
import sys
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.platforms import identity, macos_identity, macos_process, telemetry
from inferyard.platforms.identity import PreflightError

pytestmark = pytest.mark.skipif(sys.platform != "darwin", reason="Native Darwin identity test")


def test_native_cpu_string_matches_same_sysctl_key():
    from inferyard.platforms.macos_native import sysctl_string, sysctl_text

    value = sysctl_string("machdep.cpu.brand_string")
    assert value is not None
    assert value == sysctl_text("machdep.cpu.brand_string")
    assert sysctl_string("inferyard.nonexistent.key") is None


@pytest.fixture
def native_service(tmp_path):
    model = tmp_path / "synthetic.gguf"
    model.write_bytes(b"not-a-model: identity fixture only")
    script = """import json, socket, sys
server = socket.socket()
server.bind(('127.0.0.1', 0))
server.listen()
print(json.dumps({'port': server.getsockname()[1]}), flush=True)
if sys.stdin.readline().strip() == 'close':
    server.close()
    print('closed', flush=True)
    sys.stdin.readline()
server.close()
"""
    args = [sys.executable, "-u", "-c", script, "--model", str(model)]
    process = subprocess.Popen(
        args,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        env={**os.environ, "LLAMA_SERVER_SLOTS_DEBUG": "1"},
    )
    try:
        with selectors.DefaultSelector() as ready:
            ready.register(process.stdout, selectors.EVENT_READ)
            assert ready.select(5), "synthetic TCP child did not become ready"
        line = process.stdout.readline()
        assert line, "synthetic TCP child could not listen (check sandbox socket permission)"
        port = json.loads(line)["port"]
        config = {
            "endpoint": {
                "server_pid": process.pid,
                "process_start_ticks": identity.process_start_ticks(process.pid),
            },
            "engine": {"startup_args": args[1:], "slots_debug": True},
        }
        yield (
            process,
            config,
            identity.hash_file(model),
            identity.hash_file(Path(sys.executable)),
            port,
        )
    finally:
        if process.poll() is None:
            process.communicate("\n", timeout=5)
        else:
            process.communicate(timeout=5)


def test_native_child_listener_identity_memory_and_loaded_executable(native_service, monkeypatch):
    process, config, model, engine, port = native_service
    queries = []
    original = macos_identity.lsof_records

    def read(pid, selectors):
        queries.append((pid, selectors))
        return original(pid, selectors)

    monkeypatch.setattr(macos_identity, "lsof_records", read)
    observed = identity.verify_process(config, model, engine, "127.0.0.1", port)
    assert queries == [(process.pid, [])]
    assert observed["binary"] == observed["endpoint"] == "verified"
    assert observed["slots_debug_environment_verified"] is True
    assert observed["listener_source"] == "lsof:TCP:LISTEN:pid"
    assert macos_process.mapped_file(process.pid, engine)
    samples = telemetry.sample_memory(process.pid, observed["start_ticks"], "formal", "synthetic")
    assert all(row["value"] > 0 and row["missing_reason"] is None for row in samples)
    with pytest.raises(PreflightError, match="endpoint_pid_mismatch"):
        identity.verify_listener(os.getpid(), "127.0.0.1", port)


def test_native_closed_listener_is_rejected_without_pid_change(native_service):
    process, config, model, engine, port = native_service
    identity.verify_process(config, model, engine, "127.0.0.1", port)
    process.stdin.write("close\n")
    process.stdin.flush()
    with selectors.DefaultSelector() as ready:
        ready.register(process.stdout, selectors.EVENT_READ)
        assert ready.select(5), "synthetic child did not close its listener"
    assert process.stdout.readline().strip() == "closed"
    assert identity.process_start_ticks(process.pid) == config["endpoint"]["process_start_ticks"]
    with pytest.raises(PreflightError, match="endpoint_pid_mismatch"):
        identity.verify_process(config, model, engine, "127.0.0.1", port)


@pytest.mark.parametrize(
    "change,reason",
    [
        ("pid", "identity_changed"),
        ("start", "identity_changed"),
        ("exe", "binary_mismatch"),
        ("args", "arguments_mismatch"),
        ("model", "model_argument_mismatch"),
    ],
)
def test_native_wrong_binding_is_rejected(native_service, tmp_path, change, reason):
    _, original, model, engine, port = native_service
    config = deepcopy(original)
    if change == "pid":
        config["endpoint"]["server_pid"] = os.getpid()
    elif change == "start":
        config["endpoint"]["process_start_ticks"] += 1
    elif change == "exe":
        engine = model
    elif change == "args":
        config["engine"]["startup_args"] = []
    elif change == "model":
        other = tmp_path / "other.gguf"
        other.write_bytes(b"another synthetic model")
        model = identity.hash_file(other)
    with pytest.raises(PreflightError, match=reason):
        identity.verify_process(config, model, engine, "127.0.0.1", port)


def test_reaped_child_is_unavailable(native_service):
    process, *_ = native_service
    process.communicate("\n", timeout=5)
    with pytest.raises(PreflightError, match="service_process_unavailable"):
        macos_identity.process_start_ticks(process.pid)


def test_native_pid_identity_survives_psutil_wall_clock_correction(native_service, monkeypatch):
    import psutil
    from psutil import _psosx

    process, config, *_ = native_service
    before = psutil.Process(process.pid).create_time()
    monkeypatch.setattr(_psosx, "boot_time", lambda: _psosx.INIT_BOOT_TIME + 3600)
    assert psutil.Process(process.pid).create_time() != before
    assert identity.process_start_ticks(process.pid) == config["endpoint"]["process_start_ticks"]
