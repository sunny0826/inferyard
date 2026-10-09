"""Native Windows identity and evidence checks against owned synthetic processes."""

import asyncio
import os
import signal
import subprocess
import sys
from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace

import pytest

import inferyard.platforms.identity as identity
from inferyard.analysis.environment import assess_environment
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes, local_file
from inferyard.platforms.identity import (
    PreflightError,
    environment_snapshot,
    hash_file,
    memory_available,
    process_start_ticks,
    static_preflight,
    verify_listener,
    verify_process,
)
from inferyard.platforms.platform_io import open_nofollow
from inferyard.platforms.telemetry import read_rss, sample_memory
from inferyard.runtime.signals import install_termination_handler
from tests.helpers import readline_timeout

pytestmark = pytest.mark.skipif(sys.platform != "win32", reason="Native Windows APIs")


@pytest.fixture
def service(tmp_path):
    model = tmp_path / "模拟 model.gguf"
    model.write_bytes(b"synthetic model, not inference weights")
    worker = (
        "import socket,time; s=socket.socket(); s.bind(('127.0.0.1',0)); "
        "s.listen(); print(s.getsockname()[1],flush=True); time.sleep(30)"
    )
    arguments = ["-c", worker, "--model", str(model)]
    process = subprocess.Popen(
        [sys._base_executable, *arguments],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
    )
    try:
        port = int(readline_timeout(process))
        config = {
            "endpoint": {
                "server_pid": process.pid,
                "process_start_ticks": process_start_ticks(process.pid),
            },
            "engine": {"startup_args": arguments},
        }
        # venv launchers can use a different interpreter executable internally.
        import psutil

        engine = hash_file(Path(psutil.Process(process.pid).exe()))
        yield process, port, config, hash_file(model), engine
    finally:
        if process.poll() is None:
            process.kill()
        process.wait(timeout=5)
        process.stdout.close()
        process.stderr.close()


def test_native_process_listener_and_working_set_identity(service):
    process, port, config, model, engine = service
    result = verify_process(config, model, engine, "127.0.0.1", port)
    assert result["binary"] == result["endpoint"] == "verified"
    assert result["listener_inode"] is None
    assert result["process_start_source"].startswith("GetProcessTimes:")
    assert result["model_mapping"] == "not_observed"
    ticks = config["endpoint"]["process_start_ticks"]
    assert process_start_ticks(process.pid) == ticks
    assert ticks > 10**17  # Integer FILETIME, without Unix timestamp float rounding.
    rss, reason = read_rss(process.pid, ticks)
    assert rss > 0 and reason is None
    assert read_rss(process.pid, ticks + 1) == (None, "source_changed")
    samples = sample_memory(process.pid, ticks, "formal", "request-1")
    assert all(s["value"] > 0 and s["missing_reason"] is None for s in samples)
    assert samples[1]["source"] == "GetProcessMemoryInfo:WorkingSetSize"
    assert samples[0]["source"] == "GlobalMemoryStatusEx:ullAvailPhys"


@pytest.mark.parametrize("changed", ["pid", "ticks", "engine", "args", "model"])
def test_wrong_native_identity_blocks(service, changed):
    process, port, original, model, engine = service
    config = deepcopy(original)
    if changed == "pid":
        with pytest.raises(PreflightError, match="endpoint_pid_mismatch"):
            verify_listener(os.getpid(), "127.0.0.1", port)
        return
    if changed == "ticks":
        config["endpoint"]["process_start_ticks"] += 1
    elif changed == "engine":
        engine = model
    elif changed == "args":
        config["engine"]["startup_args"] = []
    else:
        model = engine
    with pytest.raises(PreflightError):
        verify_process(config, model, engine, "127.0.0.1", port)


def test_environment_unknowns_do_not_qualify_performance():
    state = environment_snapshot()
    assert state["platform"] == "Windows"
    assert state["memory_total_bytes"] > 0 and memory_available() > 0
    assert state["governor"] is None and state["swap_pages"] == {}
    assert state["evidence_durability"]["directory_fsync"] is False
    assert not assess_environment(state, state, [], {}, [])["stable_observed_environment"]


def test_declared_cuda_without_a_loaded_cuda_library_is_rejected(service):
    _, port, original, model, engine = service
    config = deepcopy(original)
    config["engine"]["backend"] = "cuda"
    with pytest.raises(PreflightError, match="cuda_backend_library_not_loaded"):
        verify_process(config, model, engine, "127.0.0.1", port)


@pytest.mark.parametrize(
    "allow_unknown,ac_online,accepted",
    [(False, None, False), (True, None, True), (True, False, False)],
)
def test_unknown_environment_opt_in_preserves_nulls_and_rejects_known_mismatch(
    service, tmp_path, monkeypatch, allow_unknown, ac_online, accepted
):
    _, _, original, model, engine = service
    config = deepcopy(original)
    config["endpoint"]["url"] = f"http://127.0.0.1:{service[1]}"
    template = tmp_path / "template.jinja"
    template.write_bytes(b"synthetic template")
    config["model"] = {
        "local_path": str(model.path),
        "sha256": model.sha256,
        "template_path": str(template),
        "template_sha256": hash_file(template).sha256,
    }
    manifest = tmp_path / "libraries.json"
    manifest.write_bytes(json_bytes({Path(engine.path).name: engine.sha256}))
    config["engine"].update(
        binary_path=str(engine.path),
        binary_sha256=engine.sha256,
        runtime_library_manifest=str(manifest),
    )
    config["output"] = {
        "root": str(tmp_path / "results"),
        "min_available_memory_bytes": 1,
        "min_disk_bytes": 1,
    }
    config["conditions"] = {
        "ac_online": True,
        "profile": "balanced",
        "governor": "powersave",
        "epp": "balance_performance",
        "allow_unknown_environment": allow_unknown,
    }
    state = environment_snapshot()
    state.update(ac_online=ac_online, mem_available_bytes=16 * 1024**3)
    monkeypatch.setattr(identity, "environment_snapshot", lambda: state)
    if accepted:
        result, _ = static_preflight(config)
        assert result["environment"]["governor"] is None
        assert not assess_environment(state, state, [], {}, [])["stable_observed_environment"]
    else:
        with pytest.raises(PreflightError, match="frozen_environment_mismatch"):
            static_preflight(config)


def test_atomic_unicode_snapshot_no_overwrite_and_reparse_rejection(tmp_path):
    target = tmp_path / "证据.json"
    atomic_bytes(target, b"first")
    with pytest.raises(EvidenceError, match="atomic_write_failed"):
        atomic_bytes(target, b"second")
    assert target.read_bytes() == b"first"
    atomic_bytes(target, b"third", overwrite=True)
    assert target.read_bytes() == b"third"
    link = tmp_path / "link.json"
    try:
        link.symlink_to(target)
    except OSError:
        pytest.skip("Creating symlinks requires Developer Mode or symlink privilege")
    with pytest.raises(OSError):
        open_nofollow(link, os.O_RDONLY)


@pytest.mark.parametrize(
    "name", ["file:stream", "D:relative.json", "D:/absolute.json", "../escape"]
)
def test_windows_artifact_paths_reject_streams_and_escape(tmp_path, name):
    with pytest.raises(EvidenceError):
        local_file(tmp_path, name)


def test_proactor_signal_cancels_task_and_restores_handler():
    previous = signal.getsignal(signal.SIGTERM)

    async def run():
        restore = install_termination_handler()
        try:
            asyncio.get_running_loop().call_soon(signal.raise_signal, signal.SIGTERM)
            await asyncio.sleep(1)
        finally:
            restore()

    with pytest.raises(asyncio.CancelledError):
        asyncio.run(run())
    assert signal.getsignal(signal.SIGTERM) == previous


def test_unsupported_phase2_dispatch_blocks_before_files_or_requests(tmp_path):
    from inferyard.extensions.workflow import live
    from inferyard.runtime.trial_runner import run_trial

    with pytest.raises(PreflightError, match="windows_phase2_live_not_supported"):
        asyncio.run(live(None, None, tmp_path / "extensions"))
    with pytest.raises(PreflightError, match="windows_phase2_live_not_supported"):
        loaded = SimpleNamespace(
            config=SimpleNamespace(to_dict=lambda: {"engine": {"adapter": "llama_cpp_b11146_v1"}})
        )
        asyncio.run(run_trial(None, None, loaded, tmp_path / "trials"))
    assert not list(tmp_path.iterdir())


def test_native_windows_batch_collector_memory_and_filetime_identity(service):
    from inferyard.platforms.resources import ResourceSampler, resource_collector_id
    from inferyard.registry import collector_factory

    process, _, config, _, _ = service
    config["telemetry"] = {"interval_ms": 1000}
    snapshots = {}
    store = SimpleNamespace(snapshot=lambda name, value: snapshots.__setitem__(name, value))
    assert resource_collector_id() == "windows-resource.v1"
    sampler = ResourceSampler(store, config)
    assert isinstance(sampler, collector_factory("windows-resource.v1"))
    ticks = config["endpoint"]["process_start_ticks"]
    samples = sampler.collect(process.pid, ticks)
    assert all(row["value"] > 0 and row["missing_reason"] is None for row in samples)
    assert samples[1]["source"] == "psutil:Process.memory_info:rss"
    assert sampler.collect(process.pid, ticks + 1)[1]["missing_reason"] == "source_changed"
    assert sampler.collect(process.pid, ticks)[1]["value"] is None
    assert snapshots["collector.json"]["process_start_source"].startswith("GetProcessTimes:")
