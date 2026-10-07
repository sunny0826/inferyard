import json
import os
import sys

import pytest

import inferyard.runtime.lock as locking
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.lock import HostLock
from tests.helpers import python_worker, readline_timeout, symlink_or_skip


@pytest.fixture
def paths(tmp_path, monkeypatch):
    monkeypatch.setattr(locking, "LEGACY_ROOT", None)
    monkeypatch.setattr(locking, "LOCK_PATH", tmp_path / "host.lock")
    monkeypatch.setattr(locking, "STATE_PATH", tmp_path / "host.state.json")
    from tests.host_state_helpers import initialize

    initialize()
    if sys.platform not in ("linux", "win32", "darwin"):
        # Keep real kernel-lock/dirty-state coverage where the production process
        # identity reader is unavailable; only its service identity is synthetic.
        def fixture_start_ticks(pid):
            if pid == os.getpid():
                return 456
            raise PreflightError("service_process_unavailable")

        monkeypatch.setattr(locking, "process_start_ticks", fixture_start_ticks)
    return tmp_path


def test_single_fixed_lock_and_inode_survives_close(paths):
    with HostLock():
        inode = locking.LOCK_PATH.stat().st_ino
        with pytest.raises(PreflightError, match="host_lock_unavailable"):
            with HostLock():
                pass
    assert locking.LOCK_PATH.stat().st_ino == inode
    with HostLock():
        pass


def test_dirty_state_survives_release_and_bound_confirmation(paths):
    endpoint = {
        "url": "http://127.0.0.1:8080",
        "server_pid": os.getpid(),
        "process_start_ticks": locking.process_start_ticks(os.getpid()),
    }
    with HostLock() as lock:
        lock.dirty("run-1", endpoint, "request-1")
        token = lock.state["dirty_token"]
    with HostLock() as lock:
        assert lock.state["dirty"]
        with pytest.raises(PreflightError, match="invalid_recovery_confirmation"):
            lock.verify_manual_recovery("wrong", "restarted", endpoint)
        with pytest.raises(PreflightError, match="old_service_still_alive"):
            lock.verify_manual_recovery(token, "restarted", endpoint)
        lock.clean()
    assert json.loads(locking.STATE_PATH.read_text())["dirty"] is False


def test_symlink_lock_rejected(paths):
    target = paths / "target"
    target.touch(mode=0o600)
    locking.LOCK_PATH.unlink()  # Isolated temporary lock replaced to test rejection.
    symlink_or_skip(locking.LOCK_PATH, target)
    with pytest.raises(PreflightError):
        with HostLock():
            pass


def test_recovery_identity_permission_failure_keeps_dirty_state(paths, monkeypatch):
    with HostLock() as lock:
        lock.dirty(
            "old-run",
            {"url": "http://127.0.0.1:1", "server_pid": 123, "process_start_ticks": 456},
            "in-flight",
        )

        def unreadable(pid):
            raise PreflightError("service_identity_unreadable")

        monkeypatch.setattr(locking, "process_start_ticks", unreadable)
        with pytest.raises(PreflightError, match="service_identity_unreadable"):
            lock.verify_manual_recovery(
                lock.state["dirty_token"], "operator restarted service", {"server_pid": 321}
            )
        assert lock.state["dirty"]
    assert json.loads(locking.STATE_PATH.read_text())["dirty"]


def test_sigkill_releases_kernel_lock_but_preserves_dirty(paths):
    import subprocess

    script = """
import time
from pathlib import Path
import inferyard.runtime.lock as locking
locking.LEGACY_ROOT = None
locking.LOCK_PATH = Path(__import__('sys').argv[1])
locking.STATE_PATH = Path(__import__('sys').argv[2])
with locking.HostLock() as lock:
    lock.dirty('killed-run', {'url':'http://127.0.0.1:1','server_pid':99999999,
                            'process_start_ticks':123}, 'in-flight')
    print('ready', flush=True)
    time.sleep(30)
"""
    child = subprocess.Popen(
        python_worker(script, str(locking.LOCK_PATH), str(locking.STATE_PATH)),
        stdout=subprocess.PIPE,
        text=True,
    )
    try:
        assert readline_timeout(child, 5) == "ready"
        with pytest.raises(PreflightError, match="host_lock_unavailable"):
            with HostLock():
                pass
        child.kill()
        child.wait(timeout=5)
        with HostLock() as lock:
            assert lock.state["dirty"]
            recovery = lock.verify_manual_recovery(
                lock.state["dirty_token"],
                "fixture old process gone",
                {
                    "server_pid": os.getpid(),
                    "process_start_ticks": locking.process_start_ticks(os.getpid()),
                },
            )
            assert recovery["old_process_gone"]
            lock.clean()
            with pytest.raises(PreflightError, match="invalid_recovery_confirmation"):
                lock.verify_manual_recovery(recovery["dirty_token"], "replayed", {})
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=5)
        child.stdout.close()
