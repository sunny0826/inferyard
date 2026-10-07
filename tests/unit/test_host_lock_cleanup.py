"""Cleanup failures must not prevent attempts to release the other host mutex."""

import os

import pytest

import inferyard.runtime.lock as locking
from inferyard.platforms.identity import PreflightError


@pytest.fixture
def dual_paths(tmp_path, monkeypatch):
    legacy = tmp_path / "D"
    common = tmp_path / "ProgramData"
    legacy.mkdir()
    common.mkdir()
    monkeypatch.setattr(locking, "LEGACY_ROOT", legacy)
    monkeypatch.setattr(locking, "LOCK_PATH", common / "host.lock")
    monkeypatch.setattr(locking, "STATE_PATH", common / "host.state.json")


def close_remaining(fds, close):
    # A failed close may leave its own fd open. Do not leak injected failures
    # into the test process or retry an ambiguous close in production.
    for fd in fds:
        try:
            os.fstat(fd)
        except OSError:
            continue
        close(fd)


@pytest.mark.parametrize("failure", ["unlock", "close_before", "close_after", "both"])
@pytest.mark.parametrize("error_type", [OSError, RuntimeError, KeyboardInterrupt])
def test_exit_attempts_every_unlock_and_close_and_raises_first(
    dual_paths, monkeypatch, failure, error_type
):
    lock = locking.HostLock()
    lock.fd = lock._acquire(locking.LEGACY_ROOT / "local-ai-benchmark-host.lock")
    lock._acquire(locking.LOCK_PATH)
    fds = list(reversed(lock._fds))
    original_lock = locking._lock
    original_close = os.close
    first = error_type("first cleanup failure")
    second = OSError("later cleanup failure")
    calls = []

    def unlock(fd, *, release=False):
        calls.append(("unlock", fd))
        if fd == fds[0] and failure in {"unlock", "both"}:
            raise first
        original_lock(fd, release=release)

    def close(fd):
        calls.append(("close", fd))
        if fd == fds[0] and failure == "close_before":
            raise first
        original_close(fd)
        if fd == fds[0] and failure in {"close_after", "both"}:
            raise second if failure == "both" else first
        if fd == fds[1]:
            raise second

    try:
        with monkeypatch.context() as patch:
            patch.setattr(locking, "_lock", unlock)
            patch.setattr(locking.os, "close", close)
            with pytest.raises(error_type) as caught:
                lock.__exit__(None, None, None)
        assert caught.value is first
        assert calls == [(operation, fd) for fd in fds for operation in ("unlock", "close")]
        assert lock.fd is None and lock._fds == [] and lock._locked_fds == set()
        # Even a close error before closing cannot strand the other kernel lock.
        # Explicit unlocking also lets a new owner acquire both locations.
        check = locking.HostLock()
        check._acquire(locking.LOCK_PATH)
        check._acquire(locking.LEGACY_ROOT / "local-ai-benchmark-host.lock")
        check.__exit__(None, None, None)
    finally:
        close_remaining(fds, original_close)


@pytest.mark.parametrize("failure", ["state", "acquire", "interrupt"])
@pytest.mark.parametrize("close_before", [False, True])
def test_enter_failure_attempts_all_closes_and_preserves_original_error(
    dual_paths, monkeypatch, failure, close_before
):
    from inferyard.runtime import host_migration

    lock = locking.HostLock()
    original_lock = locking._lock
    original_close = os.close
    original_error = {
        "state": PreflightError("unsafe_host_state"),
        "acquire": OSError("second lock unavailable"),
        "interrupt": KeyboardInterrupt("state read interrupted"),
    }[failure]
    cleanup_error = RuntimeError("first close failed")
    opened = []
    closed = []
    acquired = []
    released = []
    calls = []

    def acquire(fd, *, release=False):
        if release:
            released.append(fd)
            calls.append(("unlock", fd))
            assert fd in acquired
            return original_lock(fd, release=True)
        opened.append(fd)
        if failure == "acquire" and len(opened) == 2:
            raise original_error
        original_lock(fd, release=release)
        acquired.append(fd)

    def checkpoint(stage):
        if stage == "locks":
            raise original_error

    def close(fd):
        closed.append(fd)
        calls.append(("close", fd))
        if close_before and len(closed) == 1:
            raise cleanup_error
        original_close(fd)
        raise cleanup_error if len(closed) == 1 else OSError("second close failed")

    try:
        with monkeypatch.context() as patch:
            patch.setattr(locking, "_lock", acquire)
            patch.setattr(host_migration, "_checkpoint", checkpoint)
            patch.setattr(locking, "HostLock", lambda: lock)
            patch.setattr(locking.os, "close", close)
            expected_type = KeyboardInterrupt if failure == "interrupt" else PreflightError
            with pytest.raises(expected_type) as caught:
                host_migration.migrate()
        assert len(opened) == (2 if failure == "acquire" else 3)
        assert closed == list(reversed(opened))
        assert released == list(reversed(acquired))
        assert calls == [
            (operation, fd)
            for fd in reversed(opened)
            for operation in (("unlock", "close") if fd in acquired else ("close",))
        ]
        assert lock.fd is None and lock._fds == [] and lock._locked_fds == set()
        if failure == "acquire":
            assert str(caught.value) == "host_lock_unavailable"
            assert caught.value.__cause__ is original_error
        else:
            assert caught.value is original_error
        assert (caught.value if failure == "acquire" else original_error).__notes__ == [
            "host_lock_cleanup_failed"
        ]
        with pytest.raises(OSError):
            os.fstat(opened[0])
        # Reacquisition must succeed before test teardown closes a fd whose
        # injected close failed before releasing it.
        check = locking.HostLock()
        check._acquire(locking.LOCK_PATH)
        check._acquire(locking.LEGACY_ROOT / "local-ai-benchmark-host.lock")
        check.__exit__(None, None, None)
    finally:
        close_remaining(opened, original_close)
