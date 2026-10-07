"""Windows process-object identity for lab binding.

Mac unit tests install a fake through ``override_windows_lab_api``. Fake results
are refusal fixtures, not Windows API measurements.
"""

from __future__ import annotations

from inferyard.config.engine_fit_native_sources import (
    WINDOWS_LISTENER_SOURCE,
    WINDOWS_START_SOURCE,
)
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.windows_lab_api import current_windows_lab_api
from inferyard.platforms.windows_lab_checks import (
    MAX_INT,
    argv_digest,
    cwd_digest,
    listener_identity,
    parse_origin,
    positive_int,
    reject_deadline,
    require_pid,
    require_sha256,
    same_account,
    same_windows_path,
    windows_file_ancestors,
)

BINDING_FIELDS = (
    "pid",
    "creation_filetime",
    "origin",
    "executable_path",
    "executable_sha256",
    "argv_sha256",
    "cwd_sha256",
)
PROCESS_SOURCE = "windows:process_handle.v1"
_WAIT_OBJECT_0 = 0
_WAIT_TIMEOUT = 0x102


def _map_open_error(exc: OSError) -> PreflightError:
    """WinError 87 means the PID object is gone only when OpenProcess fails."""

    if getattr(exc, "winerror", None) == 87:
        return PreflightError("lab_windows_process_missing")
    return PreflightError("lab_windows_process_unknown")


def _map_handle_error(_exc: OSError) -> PreflightError:
    """API failure on an open handle is unknown. WinError 87 is not a missing PID."""

    return PreflightError("lab_windows_process_unknown")


class ProcessHandle:
    """Query+synchronize handle. CloseHandle runs once after a successful open."""

    def __init__(self, pid: int):
        self._pid = pid
        self._api = None
        self._handle = None
        self._closed = False

    def __enter__(self):
        require_pid(self._pid)
        self._api = current_windows_lab_api()
        try:
            handle = self._api.open_process(self._pid)
        except OSError as exc:
            raise _map_open_error(exc) from exc
        if handle is None:
            raise PreflightError("lab_windows_process_unknown")
        self._handle = handle
        return self

    def __exit__(self, exc_type, exc, tb):
        self.close()
        return False

    def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        handle, self._handle = self._handle, None
        if handle is None or self._api is None:
            return
        try:
            self._api.close_handle(handle)
        except OSError as exc:
            raise _map_handle_error(exc) from exc

    def creation_filetime(self) -> int:
        handle = self._require()
        try:
            value = self._api.creation_filetime(handle)
        except OSError as exc:
            raise _map_handle_error(exc) from exc
        if type(value) is not int or not 0 < value <= MAX_INT:
            raise PreflightError("lab_windows_process_unknown")
        return value

    def is_exited(self) -> bool:
        handle = self._require()
        try:
            result = self._api.wait_result(handle)
        except OSError as exc:
            raise _map_handle_error(exc) from exc
        if type(result) is not int:
            raise PreflightError("lab_windows_process_unknown")
        if result == _WAIT_OBJECT_0:
            return True
        if result == _WAIT_TIMEOUT:
            return False
        raise PreflightError("lab_windows_process_unknown")

    def _require(self):
        if self._closed or self._handle is None or self._api is None:
            raise PreflightError("lab_windows_process_unknown")
        return self._handle


def inspect_process(pid: int, expected_filetime: int, *, deadline: float) -> dict:
    reject_deadline(deadline)
    require_pid(pid)
    positive_int(expected_filetime, "lab_windows_invalid_filetime")
    with ProcessHandle(pid) as handle:
        reject_deadline(deadline)
        actual = handle.creation_filetime()
        reject_deadline(deadline)
        if actual != expected_filetime:
            raise PreflightError("lab_windows_filetime_mismatch")
        exited = handle.is_exited()
        reject_deadline(deadline)
    reject_deadline(deadline)
    return {
        "pid": pid,
        "creation_filetime": actual,
        "exited": exited,
        "source": PROCESS_SOURCE,
    }


def verify_process_binding(
    expected: dict, *, deadline: float, bound_executable=None, inspect_details=None
) -> dict:
    reject_deadline(deadline)
    fields = _binding(expected)
    with ProcessHandle(fields["pid"]) as primary:
        reject_deadline(deadline)
        created = primary.creation_filetime()
        reject_deadline(deadline)
        if created != fields["creation_filetime"]:
            raise PreflightError("lab_windows_filetime_mismatch")
        if primary.is_exited():
            raise PreflightError("lab_windows_process_exited")
        reject_deadline(deadline)
        with ProcessHandle(fields["pid"]) as confirm:
            _same_object(primary, confirm, created, deadline)
            _details(fields, deadline, bound_executable)
            details = inspect_details() if inspect_details is not None else None
            if bound_executable is not None and not bound_executable.unchanged():
                raise PreflightError("identity_file_changed")
            _same_object(primary, confirm, created, deadline)
        reject_deadline(deadline)
        if primary.creation_filetime() != created:
            raise PreflightError("lab_windows_pid_reused")
        reject_deadline(deadline)
        if primary.is_exited():
            raise PreflightError("lab_windows_process_exited")
        reject_deadline(deadline)
    reject_deadline(deadline)
    result = {key: expected[key] for key in BINDING_FIELDS}
    if inspect_details is not None:
        result["details"] = details
    result["listener_identity"] = fields["listener_identity"]
    result["listener_source"] = WINDOWS_LISTENER_SOURCE
    result["process_start_source"] = WINDOWS_START_SOURCE
    reject_deadline(deadline)
    return result


def _same_object(
    primary: ProcessHandle, confirm: ProcessHandle, created: int, deadline: float
) -> None:
    if primary.creation_filetime() != created or confirm.creation_filetime() != created:
        raise PreflightError("lab_windows_pid_reused")
    reject_deadline(deadline)
    if primary.is_exited() or confirm.is_exited():
        raise PreflightError("lab_windows_process_exited")
    reject_deadline(deadline)


def _details(fields: dict, deadline: float, bound_executable=None) -> None:
    api = current_windows_lab_api()
    pid = fields["pid"]
    try:
        reject_deadline(deadline)
        same_account(api.current_account(), api.process_account(pid))
        if not same_windows_path(api.process_executable(pid), fields["executable_path"]):
            raise PreflightError("lab_windows_binding_mismatch")
        if argv_digest(api.process_argv(pid)) != fields["argv_sha256"]:
            raise PreflightError("lab_windows_hash_mismatch")
        if cwd_digest(api.process_cwd(pid)) != fields["cwd_sha256"]:
            raise PreflightError("lab_windows_hash_mismatch")
        fields["listener_identity"] = listener_identity(
            fields["address"], fields["port"], pid, api.tcp_listeners()
        )
        reject_deadline(deadline)
        if bound_executable is None:
            digest = api.executable_sha256(fields["executable_path"])
        else:
            if (
                not same_windows_path(bound_executable.path, fields["executable_path"])
                or bound_executable.sha256 != fields["executable_sha256"]
                or not bound_executable.unchanged()
            ):
                raise PreflightError("identity_file_changed")
            digest = bound_executable.sha256
        reject_deadline(deadline)
    except OSError as exc:
        raise PreflightError("lab_windows_identity_incomplete") from exc
    if require_sha256(digest, "lab_windows_executable_unreadable") != fields["executable_sha256"]:
        raise PreflightError("lab_windows_hash_mismatch")


def _binding(expected: dict) -> dict:
    if type(expected) is not dict or set(expected) != set(BINDING_FIELDS):
        raise PreflightError("lab_windows_binding_invalid")
    address, port = parse_origin(expected["origin"])
    executable = expected["executable_path"]
    try:
        windows_file_ancestors(executable)
    except PreflightError as exc:
        raise PreflightError("lab_windows_binding_invalid") from exc
    return {
        "pid": require_pid(expected["pid"]),
        "creation_filetime": positive_int(
            expected["creation_filetime"], "lab_windows_invalid_filetime"
        ),
        "address": address,
        "port": port,
        "executable_path": executable,
        "executable_sha256": require_sha256(
            expected["executable_sha256"], "lab_windows_binding_invalid"
        ),
        "argv_sha256": require_sha256(expected["argv_sha256"], "lab_windows_binding_invalid"),
        "cwd_sha256": require_sha256(expected["cwd_sha256"], "lab_windows_binding_invalid"),
    }
