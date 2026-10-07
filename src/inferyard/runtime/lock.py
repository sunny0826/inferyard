"""Fixed host-wide lock and crash-persistent dirty marker on Linux and Windows."""

from __future__ import annotations

import os
import stat
import uuid
from pathlib import Path

from inferyard.evidence.storage import atomic_bytes, json_bytes, read_json
from inferyard.platforms.identity import PreflightError, process_start_ticks
from inferyard.platforms.platform_io import is_reparse, open_nofollow

if os.name == "nt":
    from inferyard.platforms.windows_host_paths import common_data_root

    _HOST_ROOT = common_data_root()
else:
    _HOST_ROOT = Path("/var/tmp")
LOCK_PATH = _HOST_ROOT / "local-ai-benchmark-host.lock"
STATE_PATH = _HOST_ROOT / "local-ai-benchmark-host.state.json"
LEGACY_ROOT = Path("D:/") if os.name == "nt" else None
# Host mutex state is not a measurement document. Keep its persisted format so
# an upgrade cannot forget an existing dirty service or bypass manual recovery.
LOCK_FORMAT_VERSION = 1


def _safe_file(info):
    if not stat.S_ISREG(info.st_mode) or is_reparse(info):
        return False
    if os.name == "nt":
        return True  # NTFS ACLs, rather than POSIX uid/mode, control access.
    return info.st_uid == os.getuid() and stat.S_IMODE(info.st_mode) == 0o600


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

    def _paths(self):
        pairs = [(LOCK_PATH, STATE_PATH)]
        if LEGACY_ROOT is not None:
            try:
                LEGACY_ROOT.stat()
            except FileNotFoundError:
                pass  # Old versions cannot acquire D:/ either when the drive is absent.
            else:
                pairs.insert(
                    0,
                    (
                        LEGACY_ROOT / "local-ai-benchmark-host.lock",
                        LEGACY_ROOT / "local-ai-benchmark-host.state.json",
                    ),
                )
        return pairs

    def _read_state(self, path):
        try:
            info = path.lstat()
        except FileNotFoundError:
            return None
        # Path.exists() suppresses permission errors in Python 3.14. An
        # unreadable old dirty marker must never look like an absent marker.
        if not _safe_file(info):
            raise PreflightError("unsafe_host_state")
        state = read_json(path)
        if (
            not isinstance(state, dict)
            or state.get("schema_version") != LOCK_FORMAT_VERSION
            or type(state.get("dirty")) is not bool
        ):
            raise PreflightError("invalid_host_state")
        return state

    def __enter__(self):
        try:
            pairs = self._paths()
            for path, _ in pairs:
                fd = open_nofollow(path, os.O_RDWR | os.O_CREAT)
                self._fds.append(fd)
                if not _safe_file(os.fstat(fd)):
                    raise PreflightError("unsafe_host_lock")
                _lock(fd)
                self._locked_fds.add(fd)
            self._state_paths = [path for _, path in pairs]
            states = [state for path in self._state_paths if (state := self._read_state(path))]
            dirty = [state for state in states if state["dirty"]]
            if dirty and any(state != dirty[0] for state in dirty[1:]):
                raise PreflightError("conflicting_host_dirty_states")
            self.state = dirty[0] if dirty else next(iter(states), None)
            self.fd = self._fds[-1]
            # Repair an interrupted mirror write while both kernel locks are held.
            # In particular, publish new-location dirty to D:/ before releasing it.
            if self.state is not None and len(pairs) > 1:
                self.write(self.state)
            return self
        except BaseException as exc:
            cleanup_error = self._release_fds()
            if cleanup_error is not None:
                exc.add_note(f"host_lock_cleanup_failed: {cleanup_error!r}")
            if isinstance(exc, OSError):
                raise PreflightError("host_lock_unavailable") from exc
            raise

    def write(self, state):
        if self.fd is None:
            raise PreflightError("host_lock_not_held")
        for path in self._state_paths:
            atomic_bytes(path, json_bytes(state), overwrite=True)
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
        return error

    def __exit__(self, *args):
        error = self._release_fds()
        if error is not None:
            raise error
