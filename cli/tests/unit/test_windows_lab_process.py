"""Mac rejection tests for the Windows lab process handle and binding."""

import hashlib
import time
from types import SimpleNamespace

import pytest

from inferyard.errors import PreflightError
from inferyard.platforms.windows_lab_api import override_windows_lab_api
from inferyard.platforms.windows_lab_process import verify_process_binding

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


def test_details_scan_cannot_hide_pid_reuse_exit_or_file_replacement():
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
