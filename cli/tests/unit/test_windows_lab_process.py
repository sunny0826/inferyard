"""Mac rejection tests for the Windows lab process handle and binding."""

import hashlib
import json
import time

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms import windows_lab_checks as checks
from inferyard.platforms.windows_lab_api import override_windows_lab_api
from inferyard.platforms.windows_lab_process import (
    ProcessHandle,
    verify_process_binding,
)

ARGV = [r"C:\engine\server.exe", "--port", "8080"]
CWD = r"C:\work"
SECRET = "SUPER_SECRET_ARGV"
ACCOUNT = "SUPER_SECRET_USER"


def _winerror(code):
    error = OSError("windows api failed")
    error.winerror = code
    return error


def _digest_argv(arguments):
    raw = b"\0".join(argument.encode("utf-8") for argument in arguments) + b"\0"
    return hashlib.sha256(raw).hexdigest()


class Proc:
    def __init__(self):
        self.handles = 0
        self.closed = []
        self.fail_open = None
        self.filetime = 100
        self.filetime_for = {}
        self.wait = 0x102
        self.wait_error = None
        self.time_error = None
        self.close_error = None
        self.account = ACCOUNT
        self.current = ACCOUNT
        self.exe = r"C:\engine\server.exe"
        self.argv = list(ARGV) + [SECRET]
        self.cwd = CWD
        self.listeners = [("127.0.0.1", 8080, 42)]
        self.sha = "4e17d37310243af5edd93af901d103e1522035fc6dd10cbaaf8558dfdf0f8200"

    def open_process(self, pid):
        if self.fail_open:
            raise _winerror(self.fail_open)
        self.handles += 1
        return self.handles

    def close_handle(self, handle):
        if handle in self.closed:
            raise AssertionError("double close")
        self.closed.append(handle)
        if self.close_error:
            raise _winerror(self.close_error)

    def creation_filetime(self, handle):
        if self.time_error:
            raise _winerror(self.time_error)
        return self.filetime_for.get(handle, self.filetime)

    def wait_result(self, handle):
        if self.wait_error:
            raise _winerror(self.wait_error)
        return self.wait

    def current_account(self):
        return self.current

    def process_account(self, pid):
        return self.account

    def process_executable(self, pid):
        return self.exe

    def process_argv(self, pid):
        return list(self.argv)

    def process_cwd(self, pid):
        return self.cwd

    def tcp_listeners(self):
        return list(self.listeners)

    def executable_sha256(self, path):
        return self.sha


def _expected(proc):
    return {
        "pid": 42,
        "creation_filetime": 100,
        "origin": "http://127.0.0.1:8080",
        "executable_path": proc.exe,
        "executable_sha256": proc.sha,
        "argv_sha256": _digest_argv(proc.argv),
        "cwd_sha256": hashlib.sha256(proc.cwd.encode("utf-8")).hexdigest(),
    }


def _soon():
    return time.monotonic() + 30


def _raises(proc, call):
    with override_windows_lab_api(proc), pytest.raises(PreflightError) as caught:
        call()
    return caught.value


def test_open_failure_distinguishes_missing_from_unknown_and_does_not_close():
    missing = Proc()
    missing.fail_open = 87
    unknown = Proc()
    unknown.fail_open = 5
    assert _raises(
        missing, lambda: verify_process_binding(_expected(missing), deadline=_soon())
    ).args == ("lab_windows_process_missing",)
    assert _raises(
        unknown, lambda: verify_process_binding(_expected(unknown), deadline=_soon())
    ).args == ("lab_windows_process_unknown",)
    assert missing.closed == [] and unknown.closed == []


def test_filetime_mismatch_is_not_reported_as_missing():
    proc = Proc()
    proc.filetime = 50
    error = _raises(proc, lambda: verify_process_binding(_expected(proc), deadline=_soon()))
    assert error.args == ("lab_windows_filetime_mismatch",)
    assert proc.closed == [1]


def test_exited_process_is_visible_to_handle_and_rejected_by_binding():
    proc = Proc()
    proc.wait = 0
    with override_windows_lab_api(proc), ProcessHandle(4) as handle:
        assert handle.is_exited() is True
    assert proc.closed == [1]
    error = _raises(proc, lambda: verify_process_binding(_expected(proc), deadline=_soon()))
    assert error.args == ("lab_windows_process_exited",)
    assert SECRET not in str(error) and ACCOUNT not in str(error)


def test_pid_reuse_rejects_binding_and_releases_handles():
    proc = Proc()
    proc.filetime_for = {2: 200}
    error = _raises(proc, lambda: verify_process_binding(_expected(proc), deadline=_soon()))
    assert error.args == ("lab_windows_pid_reused",)
    assert set(proc.closed) == set(range(1, proc.handles + 1))
    assert SECRET not in str(error)


def test_binding_returns_exact_fields_and_hides_argv_account():
    proc = Proc()
    proc.current = "secret\\User"
    proc.account = "SECRET\\user"
    original = _expected(proc)
    with override_windows_lab_api(proc):
        result = verify_process_binding(original, deadline=_soon())
    assert result["listener_identity"] == "windows:tcp:127.0.0.1:8080:pid:42"
    assert result["listener_source"] == "GetExtendedTcpTable:owner_pid"
    assert result["process_start_source"] == "GetProcessTimes:creation_FILETIME_100ns_since_1601"
    encoded = json.dumps(result)
    assert SECRET not in encoded and ACCOUNT not in encoded and "secret\\User" not in encoded


@pytest.mark.parametrize(
    ("origin", "listeners", "code"),
    [
        ("http://127.0.0.1:8080", [("0.0.0.0", 8080, 42)], "lab_windows_listener_ambiguous"),
        (
            "http://127.0.0.1:8080",
            [("127.0.0.1", 8080, 42), ("::", 8080, 42)],
            "lab_windows_listener_ambiguous",
        ),
        ("http://[::1]:8080", [("::", 8080, 42)], "lab_windows_listener_ambiguous"),
        ("http://127.0.0.1:8080", [("127.0.0.1", 8080, 99)], "lab_windows_listener_mismatch"),
        ("http://127.0.0.1:8080", [("127.0.0.1", 8080, None)], "lab_windows_identity_incomplete"),
        ("http://localhost:8080", [("127.0.0.1", 8080, 42)], "lab_windows_origin_invalid"),
        (
            "http://user:secret@127.0.0.1:8080",
            [("127.0.0.1", 8080, 42)],
            "lab_windows_origin_invalid",
        ),
        (
            "http://127.0.0.1:8080?token=secret",
            [("127.0.0.1", 8080, 42)],
            "lab_windows_origin_invalid",
        ),
        ("http://0.0.0.0:8080", [("0.0.0.0", 8080, 42)], "lab_windows_origin_invalid"),
    ],
)
def test_listener_and_origin_rejections(origin, listeners, code):
    proc = Proc()
    proc.listeners = listeners
    expected = _expected(proc)
    expected["origin"] = origin
    error = _raises(proc, lambda: verify_process_binding(expected, deadline=_soon()))
    assert error.args == (code,)
    assert "secret" not in str(error)
    if code == "lab_windows_origin_invalid":
        assert proc.closed == []
    else:
        assert proc.closed


def test_account_hash_and_path_mismatches_do_not_echo_secrets():
    proc = Proc()
    proc.account = "other"
    error = _raises(proc, lambda: verify_process_binding(_expected(proc), deadline=_soon()))
    assert error.args == ("lab_windows_account_mismatch",)
    assert ACCOUNT not in str(error)

    proc = Proc()
    expected = _expected(proc)
    proc.argv = ["other"]
    error = _raises(proc, lambda: verify_process_binding(expected, deadline=_soon()))
    assert error.args == ("lab_windows_hash_mismatch",)

    proc = Proc()
    expected = _expected(proc)
    proc.sha = "ab" * 32
    error = _raises(proc, lambda: verify_process_binding(expected, deadline=_soon()))
    assert error.args == ("lab_windows_hash_mismatch",)


def _hold_clock(monkeypatch):
    clock = {"expire": False}

    def monotonic():
        return 100.0 if clock["expire"] else 1.0

    monkeypatch.setattr(checks.time, "monotonic", monotonic)
    return clock


def test_binding_final_close_past_deadline_does_not_succeed(monkeypatch):
    proc = Proc()
    clock = _hold_clock(monkeypatch)
    expected = _expected(proc)
    original = proc.close_handle

    def close_handle(handle):
        original(handle)
        if len(proc.closed) == 2:
            clock["expire"] = True

    proc.close_handle = close_handle
    error = _raises(proc, lambda: verify_process_binding(expected, deadline=50.0))
    assert error.args == ("lab_windows_deadline_exceeded",)


def test_details_scan_cannot_hide_pid_reuse_exit_or_file_replacement():
    from types import SimpleNamespace

    for change in ("pid", "exit", "file"):
        proc = Proc()
        unchanged = [True]
        bound = SimpleNamespace(
            path=proc.exe, sha256=proc.sha, unchanged=lambda unchanged=unchanged: unchanged[0]
        )

        def details(proc=proc, change=change, unchanged=unchanged):
            assert proc.closed == []
            if change == "pid":
                proc.filetime_for[2] = 101
            elif change == "exit":
                proc.wait = 0
            else:
                unchanged[0] = False

        with override_windows_lab_api(proc), pytest.raises(PreflightError):
            verify_process_binding(
                _expected(proc), deadline=_soon(), bound_executable=bound, inspect_details=details
            )
