"""Fixed host-wide lock and crash-persistent dirty marker on Linux and Windows."""

from __future__ import annotations

import os
import uuid
from pathlib import Path

from inferyard.platforms.identity import PreflightError, process_start_ticks
from inferyard.platforms.platform_io import open_nofollow
from inferyard.runtime.host_files import (
    decode,
    identity,
    publish_state,
    read_bytes,
    root_identity,
    safe_file,
    validate_state,
)

if os.name == "nt":
    from inferyard.platforms.windows_host_paths import common_data_root

    _HOST_ROOT = common_data_root()
else:
    _HOST_ROOT = Path("/var/tmp")
LOCK_PATH = _HOST_ROOT / "inferyard-host.lock"
STATE_PATH = _HOST_ROOT / "inferyard-host.state.json"
# Host mutex state is not a measurement document. Keep its persisted format so
# an upgrade cannot forget an existing dirty service or bypass manual recovery.
LOCK_FORMAT_VERSION = 1


_safe_file = safe_file


def _lock(fd, *, release=False):
    if os.name == "nt":
        import msvcrt

        os.lseek(fd, 0, os.SEEK_SET)
        msvcrt.locking(fd, msvcrt.LK_UNLCK if release else msvcrt.LK_NBLCK, 1)
    else:
        import fcntl

        fcntl.flock(fd, fcntl.LOCK_UN if release else fcntl.LOCK_EX | fcntl.LOCK_NB)


class HostLock:
    def __init__(self):
        self.fd = None
        self.state = None
        self._fds = []
        self._locked_fds = set()
        self._state_paths = []
        self._identities = {}
        self._created_paths = set()

    def _acquire(self, path):
        root = root_identity(path.parent)
        try:
            fd = open_nofollow(path, os.O_RDWR | os.O_CREAT | os.O_EXCL)
            self._created_paths.add(path)
        except FileExistsError:
            fd = open_nofollow(path, os.O_RDWR)
        self._fds.append(fd)
        if not _safe_file(os.fstat(fd)):
            raise PreflightError("unsafe_host_lock")
        _lock(fd)
        self._locked_fds.add(fd)
        self._identities[path] = (fd, identity(os.fstat(fd)), root)
        self._check_locks()
        return fd

    def _check_locks(self):
        for path, (fd, saved, root) in self._identities.items():
            if (
                identity(os.fstat(fd)) != saved
                or identity(path.lstat()) != saved
                or not _safe_file(path.lstat())
                or root_identity(path.parent) != root
            ):
                raise PreflightError("host_state_identity_changed")

    def _read_state(self, path):
        raw = read_bytes(path)
        return validate_state(decode(raw)) if raw is not None else None

    def __enter__(self):
        try:
            self.fd = self._acquire(LOCK_PATH)
            self._state_paths = [STATE_PATH]
            raw = read_bytes(STATE_PATH)
            if raw is None:
                publish_state(
                    STATE_PATH,
                    {"schema_version": LOCK_FORMAT_VERSION, "dirty": False},
                    overwrite=False,
                )
                raw = read_bytes(STATE_PATH)
            state = decode(raw)
            state.pop("migration", None)
            self.state = validate_state(state)
            return self
        except BaseException as exc:
            cleanup_error = self._release_fds()
            if cleanup_error is not None:
                exc.add_note("host_lock_cleanup_failed")
            if isinstance(exc, OSError):
                raise PreflightError("host_lock_unavailable") from exc
            raise

    def write(self, state):
        if self.fd is None:
            raise PreflightError("host_lock_not_held")
        self._check_locks()
        state = validate_state(state)
        publish_state(STATE_PATH, state, overwrite=True)
        self.state = state

    def dirty(self, run_id, endpoint, request_id, *, kind="run"):
        self.write(
            {
                "schema_version": LOCK_FORMAT_VERSION,
                "dirty": True,
                "dirty_token": uuid.uuid4().hex,
                "run_id": run_id,
                "kind": kind,
                "endpoint": endpoint["url"],
                "server_pid": endpoint["server_pid"],
                "process_start_ticks": endpoint["process_start_ticks"],
                "request_id": request_id,
            }
        )

    def clean(self):
        if self.state:
            self.write({**self.state, "dirty": False})

    def verify_manual_recovery(self, token, note, new_endpoint):
        old = self.state
        if (
            not old
            or not old["dirty"]
            or token != old.get("dirty_token")
            or not note
            or not note.strip()
        ):
            raise PreflightError("invalid_recovery_confirmation")
        try:
            still_same = process_start_ticks(old["server_pid"]) == old["process_start_ticks"]
        except PreflightError as exc:
            if str(exc) != "service_process_unavailable":
                raise
            still_same = False
        if still_same:
            raise PreflightError("old_service_still_alive")
        if (new_endpoint["server_pid"], new_endpoint["process_start_ticks"]) == (
            old["server_pid"],
            old["process_start_ticks"],
        ):
            raise PreflightError("recovery_requires_new_process")
        # Caller still must verify new process ownership and its idle slots before clean().
        return {
            "dirty_token": token,
            "note": note,
            "old_process_gone": True,
            "old_run_id": old["run_id"],
        }

    def _release_fds(self):
        error = None
        try:
            for fd in reversed(self._fds):
                # Enter may fail after opening a file but before acquiring its
                # lock. Unlock only successful acquisitions, before any close.
                if fd in self._locked_fds:
                    try:
                        _lock(fd, release=True)
                    except BaseException as exc:
                        if error is None:
                            error = exc
                try:
                    os.close(fd)
                except BaseException as exc:
                    if error is None:
                        error = exc
        finally:
            self._fds.clear()
            self._locked_fds.clear()
            self.fd = None
            self._identities.clear()
            self._created_paths.clear()
        return error

    def __exit__(self, *args):
        error = self._release_fds()
        if error is not None:
            raise error
