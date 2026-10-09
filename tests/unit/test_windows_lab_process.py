"""Mac rejection tests for the Windows lab process handle and binding."""

import hashlib
import json
import time
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.platforms import windows_lab_checks as checks
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.windows_lab_api import override_windows_lab_api
from inferyard.platforms.windows_lab_process import (
    ProcessHandle,
    verify_process_binding,
)

ROOT = Path(__file__).parents[1] / "fixtures" / "windows_lab"
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
        self.times = []
        self.waits = []
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
        self.details = 0

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
        self.times.append(handle)
        if self.time_error:
            raise _winerror(self.time_error)
        return self.filetime_for.get(handle, self.filetime)

    def wait_result(self, handle):
        self.waits.append(handle)
        if self.wait_error:
            raise _winerror(self.wait_error)
        return self.wait

    def current_account(self):
        self.details += 1
        return self.current

    def process_account(self, pid):
        self.details += 1
        return self.account

    def process_executable(self, pid):
        self.details += 1
        return self.exe

    def process_argv(self, pid):
        self.details += 1
        return list(self.argv)

    def process_cwd(self, pid):
        self.details += 1
        return self.cwd

    def tcp_listeners(self):
        self.details += 1
        return list(self.listeners)

    def executable_sha256(self, path):
        self.details += 1
        self.hashed = path
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


def test_fixture_hash_matches_strict_utf8_oracle():
    loaded = strict_json_loads(ROOT.joinpath("process_binding.json").read_text(encoding="utf-8"))
    assert loaded["argv_sha256"] == _digest_argv(ARGV)
    assert loaded["cwd_sha256"] == hashlib.sha256(CWD.encode("utf-8")).hexdigest()


def test_invalid_scalars_do_not_open_or_load_api():
    with pytest.raises(PreflightError, match="lab_windows_invalid_pid"):
        verify_process_binding({**_expected(Proc()), "pid": True}, deadline=_soon())
    with pytest.raises(PreflightError, match="lab_windows_deadline_invalid"):
        verify_process_binding(_expected(Proc()), deadline=float("nan"))
    with pytest.raises(PreflightError, match="lab_windows_invalid_filetime"):
        verify_process_binding({**_expected(Proc()), "creation_filetime": False}, deadline=_soon())


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
    assert missing.details == 0 and unknown.details == 0


def test_same_handle_times_and_wait_close_once_on_success_and_api_failure():
    proc = Proc()
    with override_windows_lab_api(proc), ProcessHandle(4) as handle:
        assert handle.creation_filetime() == 100
        assert handle.is_exited() is False
    assert proc.times == [1] and proc.waits == [1] and proc.closed == [1]

    failed = Proc()
    failed.time_error = 5
    assert _raises(
        failed, lambda: verify_process_binding(_expected(failed), deadline=_soon())
    ).args == ("lab_windows_process_unknown",)
    assert failed.times == [1] and failed.waits == [] and failed.closed == [1]

    waiting = Proc()
    waiting.wait_error = 5
    assert _raises(
        waiting, lambda: verify_process_binding(_expected(waiting), deadline=_soon())
    ).args == ("lab_windows_process_unknown",)
    assert waiting.times == [1] and waiting.waits == [1] and waiting.closed == [1]


@pytest.mark.parametrize("result", [0xFFFFFFFF, True, False])
def test_non_timeout_wait_is_unknown_not_exited(result):
    proc = Proc()
    proc.wait = result
    error = _raises(proc, lambda: verify_process_binding(_expected(proc), deadline=_soon()))
    assert error.args == ("lab_windows_process_unknown",)
    assert proc.closed == [1]


def test_filetime_mismatch_is_not_reported_as_missing():
    proc = Proc()
    proc.filetime = 50
    error = _raises(proc, lambda: verify_process_binding(_expected(proc), deadline=_soon()))
    assert error.args == ("lab_windows_filetime_mismatch",)
    assert proc.closed == [1] and proc.waits == [] and proc.details == 0


def test_exited_process_is_visible_to_handle_and_rejected_by_binding():
    proc = Proc()
    proc.wait = 0
    with override_windows_lab_api(proc), ProcessHandle(4) as handle:
        assert handle.is_exited() is True
    assert proc.closed == [1]
    error = _raises(proc, lambda: verify_process_binding(_expected(proc), deadline=_soon()))
    assert error.args == ("lab_windows_process_exited",)
    assert SECRET not in str(error) and ACCOUNT not in str(error)


def test_explicit_close_is_not_repeated_by_exit():
    proc = Proc()
    with override_windows_lab_api(proc), ProcessHandle(4) as handle:
        assert handle.creation_filetime() == 100
        handle.close()
        handle.close()
    assert proc.closed == [1]


def test_pid_reuse_closes_both_handles_without_reading_new_details():
    proc = Proc()
    proc.filetime_for = {2: 200}
    error = _raises(proc, lambda: verify_process_binding(_expected(proc), deadline=_soon()))
    assert error.args == ("lab_windows_pid_reused",)
    assert proc.closed == [2, 1]
    assert proc.details == 0
    assert SECRET not in str(error)


def test_binding_returns_exact_fields_and_hides_argv_account():
    proc = Proc()
    proc.current = "secret\\User"
    proc.account = "SECRET\\user"
    original = _expected(proc)
    snapshot = deepcopy(original)
    with override_windows_lab_api(proc):
        result = verify_process_binding(original, deadline=_soon())
    assert original == snapshot
    assert set(result) == {
        *snapshot,
        "listener_identity",
        "listener_source",
        "process_start_source",
    }
    assert result["listener_identity"] == "windows:tcp:127.0.0.1:8080:pid:42"
    assert result["listener_source"] == "GetExtendedTcpTable:owner_pid"
    assert result["process_start_source"] == "GetProcessTimes:creation_FILETIME_100ns_since_1601"
    encoded = json.dumps(result)
    assert SECRET not in encoded and ACCOUNT not in encoded and "secret\\User" not in encoded
    assert proc.closed == [2, 1]
    assert proc.times[0] == proc.waits[0]


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
    assert proc.closed == [2, 1]


def test_surrogate_argv_is_incomplete_identity():
    proc = Proc()
    expected = _expected(proc)
    proc.argv = ["\udcff"]
    error = _raises(proc, lambda: verify_process_binding(expected, deadline=_soon()))
    assert error.args == ("lab_windows_identity_incomplete",)


def test_ipv6_loopback_listener_uses_existing_source_constants():
    proc = Proc()
    proc.listeners = [("::1", 9, 42)]
    expected = _expected(proc)
    expected["origin"] = "http://[::1]:9"
    with override_windows_lab_api(proc):
        result = verify_process_binding(expected, deadline=_soon())
    assert result["listener_identity"] == "windows:tcp:::1:9:pid:42"


def test_deadline_and_shape_failures(monkeypatch):
    monkeypatch.setattr(checks.time, "monotonic", lambda: 5.0)
    with pytest.raises(PreflightError, match="lab_windows_deadline_exceeded"):
        verify_process_binding(_expected(Proc()), deadline=5.0)
    with pytest.raises(PreflightError, match="lab_windows_binding_invalid"):
        verify_process_binding({"pid": 4}, deadline=50.0)
    with pytest.raises(ContractError, match="duplicate JSON key"):
        strict_json_loads(ROOT.joinpath("duplicate_key.json").read_text(encoding="utf-8"))


@pytest.mark.parametrize(
    ("slot", "waits"),
    [("time_error", []), ("wait_error", [1])],
)
def test_opened_handle_winerror_87_is_unknown_not_missing(slot, waits):
    proc = Proc()
    setattr(proc, slot, 87)
    error = _raises(proc, lambda: verify_process_binding(_expected(proc), deadline=_soon()))
    assert error.args == ("lab_windows_process_unknown",)
    assert proc.closed == [1]
    assert proc.waits == waits


def test_close_handle_winerror_87_is_unknown_and_closes_once():
    proc = Proc()
    proc.close_error = 87
    with (
        override_windows_lab_api(proc),
        pytest.raises(PreflightError, match="lab_windows_process_unknown"),
        ProcessHandle(4),
    ):
        pass
    assert proc.closed == [1]


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
    assert proc.closed == [2, 1]


def test_bound_executable_skips_full_hash_with_details_inside_handles():
    from types import SimpleNamespace

    proc = Proc()
    checks_seen = []
    bound = SimpleNamespace(
        path=proc.exe, sha256=proc.sha, unchanged=lambda: checks_seen.append(1) or True
    )

    def details():
        assert proc.handles == 2 and proc.closed == []
        assert proc.times and proc.waits
        return ["verified argv"]

    with override_windows_lab_api(proc):
        value = verify_process_binding(
            _expected(proc), deadline=_soon(), bound_executable=bound, inspect_details=details
        )
    assert value["details"] == ["verified argv"]
    assert len(checks_seen) == 2
    assert not hasattr(proc, "hashed")
    assert proc.closed == [2, 1]


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
        assert not hasattr(proc, "hashed") and proc.closed == [2, 1]
