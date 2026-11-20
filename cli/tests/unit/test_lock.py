import asyncio
import json
import os
import sys

import pytest

import inferyard.runtime.lock as locking
from inferyard.errors import PreflightError
from inferyard.runtime.lock import HostLock
from tests.helpers import python_worker, readline_timeout, symlink_or_skip


@pytest.fixture
def paths(tmp_path, monkeypatch):
    monkeypatch.setattr(locking, "LOCK_PATH", tmp_path / "host.lock")
    monkeypatch.setattr(locking, "STATE_PATH", tmp_path / "host.state.json")
    from tests.host_state_helpers import initialize

    initialize()
    if sys.platform not in ("linux", "win32", "darwin"):
        def fixture_start_ticks(pid):
            if pid == os.getpid():
                return 456
            raise PreflightError("service_process_unavailable")

        monkeypatch.setattr(locking, "process_start_ticks", fixture_start_ticks)
    return tmp_path


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
