"""Simulate Windows drive layouts with real host kernel locks and durable states."""

import os
import subprocess
from types import SimpleNamespace

import pytest

import inferyard.runtime.lock as locking
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes, read_json
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.windows_host_paths import common_data_root
from tests.helpers import python_worker, readline_timeout


@pytest.fixture
def paths(tmp_path, monkeypatch):
    common = tmp_path / "ProgramData"
    common.mkdir()
    legacy = tmp_path / "D"
    monkeypatch.setattr(locking, "LOCK_PATH", common / "local-ai-benchmark-host.lock")
    monkeypatch.setattr(locking, "STATE_PATH", common / "local-ai-benchmark-host.state.json")
    monkeypatch.setattr(locking, "LEGACY_ROOT", legacy)
    return legacy, common


def dirty_state(token="old-token"):
    return {
        "schema_version": 1,
        "dirty": True,
        "dirty_token": token,
        "run_id": "old-run",
        "server_pid": 111,
        "process_start_ticks": 222,
    }


def test_system_folder_comes_from_native_common_appdata_not_user_environment(monkeypatch):
    seen = []

    def query(window, folder, token, flags, result):
        seen.append((window, folder, token, flags))
        result.value = "C:/ProgramData"
        return 0

    class Query:
        def __call__(self, *args):
            return query(*args)

    monkeypatch.setenv("ProgramData", "Z:/per-user")
    monkeypatch.setattr(
        "inferyard.platforms.windows_host_paths.ctypes.WinDLL",
        lambda *args, **kwargs: SimpleNamespace(SHGetFolderPathW=Query()),
        raising=False,
    )
    assert str(common_data_root()).replace("\\", "/") == "C:/ProgramData"
    assert seen == [(None, 0x0023, None, 0)]


def test_no_d_drive_uses_common_lock_and_retains_dirty(paths):
    legacy, _ = paths
    with locking.HostLock() as lock:
        lock.write(dirty_state())
        with pytest.raises(PreflightError, match="host_lock_unavailable"):
            locking.HostLock().__enter__()
    assert not legacy.exists()
    with locking.HostLock() as lock:
        assert lock.state == dirty_state()


def test_old_dirty_requires_same_manual_recovery_and_is_mirrored(paths, monkeypatch):
    legacy, _ = paths
    legacy.mkdir()
    state_path = legacy / "local-ai-benchmark-host.state.json"
    atomic_bytes(state_path, json_bytes(dirty_state()))
    monkeypatch.setattr(locking, "process_start_ticks", lambda pid: 222)
    with locking.HostLock() as lock:
        assert lock.state == dirty_state()
        assert read_json(locking.STATE_PATH) == dirty_state()
        with pytest.raises(PreflightError, match="old_service_still_alive"):
            lock.verify_manual_recovery("old-token", "restarted", {"server_pid": 333})

        def gone(pid):
            raise PreflightError("service_process_unavailable")

        monkeypatch.setattr(locking, "process_start_ticks", gone)
        assert lock.verify_manual_recovery(
            "old-token", "restarted", {"server_pid": 333, "process_start_ticks": 444}
        )["old_process_gone"]
        lock.clean()
    assert not read_json(state_path)["dirty"]
    assert read_json(state_path) == read_json(locking.STATE_PATH)


def test_old_version_lock_and_new_version_exclude_each_other(paths):
    legacy, _ = paths
    legacy.mkdir()
    old_path = legacy / "local-ai-benchmark-host.lock"
    fd = locking.open_nofollow(old_path, os.O_RDWR | os.O_CREAT)
    try:
        locking._lock(fd)
        candidate = locking.HostLock()
        with pytest.raises(PreflightError, match="host_lock_unavailable"):
            candidate.__enter__()
        assert not candidate._fds and candidate.fd is None
    finally:
        os.close(fd)
    with locking.HostLock():
        fd = locking.open_nofollow(old_path, os.O_RDWR)
        try:
            with pytest.raises(OSError):
                locking._lock(fd)
        finally:
            os.close(fd)


def test_legacy_process_crash_preserves_dirty_and_releases_both_locations(paths):
    legacy, _ = paths
    legacy.mkdir()
    script = """
import sys, time
from pathlib import Path
import inferyard.runtime.lock as locking
locking.LEGACY_ROOT = None
locking.LOCK_PATH = Path(sys.argv[1]) / 'local-ai-benchmark-host.lock'
locking.STATE_PATH = Path(sys.argv[1]) / 'local-ai-benchmark-host.state.json'
with locking.HostLock() as lock:
    lock.dirty('legacy-crash', {'url': 'http://127.0.0.1:1', 'server_pid': 111,
                              'process_start_ticks': 222}, 'pending')
    print('ready', flush=True)
    time.sleep(30)
"""
    child = subprocess.Popen(python_worker(script, str(legacy)), stdout=subprocess.PIPE, text=True)
    try:
        assert readline_timeout(child, 5) == "ready"
        with pytest.raises(PreflightError, match="host_lock_unavailable"):
            locking.HostLock().__enter__()
        child.kill()
        child.wait(timeout=5)
        with locking.HostLock() as lock:
            assert lock.state["dirty"] and lock.state["run_id"] == "legacy-crash"
            assert read_json(locking.STATE_PATH) == lock.state
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=5)
        child.stdout.close()


@pytest.mark.parametrize("operation", ["dirty", "clean"])
def test_interrupted_mirror_write_never_forgets_dirty_and_releases_all_locks(
    paths, monkeypatch, operation
):
    legacy, _ = paths
    legacy.mkdir()
    original = locking.atomic_bytes
    with locking.HostLock() as lock:
        if operation == "clean":
            lock.write(dirty_state())

        def interrupted(path, content, **kwargs):
            if path == locking.STATE_PATH:
                raise EvidenceError("simulated_crash")
            original(path, content, **kwargs)

        monkeypatch.setattr(locking, "atomic_bytes", interrupted)
        with pytest.raises(EvidenceError, match="simulated_crash"):
            lock.clean() if operation == "clean" else lock.write(dirty_state())
    monkeypatch.setattr(locking, "atomic_bytes", original)
    with locking.HostLock() as lock:
        assert lock.state["dirty"]
    assert read_json(legacy / "local-ai-benchmark-host.state.json") == read_json(locking.STATE_PATH)


def test_conflicting_dirty_states_fail_closed_and_unlock(paths):
    legacy, _ = paths
    legacy.mkdir()
    atomic_bytes(legacy / "local-ai-benchmark-host.state.json", json_bytes(dirty_state()))
    atomic_bytes(locking.STATE_PATH, json_bytes(dirty_state("different-token")))
    lock = locking.HostLock()
    with pytest.raises(PreflightError, match="conflicting_host_dirty_states"):
        lock.__enter__()
    assert lock.fd is None and lock._fds == []


def test_unreadable_existing_d_drive_is_not_treated_as_absent(paths, monkeypatch):
    legacy, _ = paths
    original = type(legacy).stat

    def stat(path, *args, **kwargs):
        if path == legacy:
            raise PermissionError("D unavailable")
        return original(path, *args, **kwargs)

    monkeypatch.setattr(type(legacy), "stat", stat)
    with pytest.raises(PreflightError, match="host_lock_unavailable"):
        locking.HostLock().__enter__()


def test_unreadable_legacy_state_is_not_ignored_and_all_locks_are_released(paths, monkeypatch):
    legacy, _ = paths
    legacy.mkdir()
    state_path = legacy / "local-ai-benchmark-host.state.json"
    atomic_bytes(state_path, json_bytes(dirty_state()))
    original = type(state_path).stat
    original_lstat = type(state_path).lstat

    def stat(path, *args, **kwargs):
        if path == state_path:
            raise PermissionError("old state unreadable")
        return original(path, *args, **kwargs)

    monkeypatch.setattr(type(state_path), "stat", stat)

    def lstat(path, *args, **kwargs):
        if path == state_path:
            raise PermissionError("old state unreadable")
        return original_lstat(path, *args, **kwargs)

    monkeypatch.setattr(type(state_path), "lstat", lstat)
    lock = locking.HostLock()
    with pytest.raises(PreflightError, match="host_lock_unavailable"):
        lock.__enter__()
    assert lock.fd is None and not lock._fds
    monkeypatch.setattr(type(state_path), "stat", original)
    monkeypatch.setattr(type(state_path), "lstat", original_lstat)
    assert read_json(state_path) == dirty_state()
    with locking.HostLock() as lock:
        assert lock.state == dirty_state()
