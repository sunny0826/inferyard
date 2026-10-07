"""Injectable Windows lab API. WinDLL loads only on a real Windows call."""

from __future__ import annotations

import contextvars
import sys
from contextlib import contextmanager

from inferyard.platforms.identity import PreflightError

PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
SYNCHRONIZE = 0x00100000
PROCESS_ACCESS = PROCESS_QUERY_LIMITED_INFORMATION | SYNCHRONIZE
WAIT_FAILED = 0xFFFFFFFF

_CURRENT: contextvars.ContextVar = contextvars.ContextVar("windows_lab_api", default=None)
_REAL = None


def _bounded_open_process(open_process, pid: int):
    """Open with query+synchronize only. ``open_process`` receives that mask."""

    from inferyard.platforms.windows_lab_checks import require_pid

    require_pid(pid)
    return open_process(PROCESS_ACCESS, False, pid)


@contextmanager
def override_windows_lab_api(api):
    """Install a fake API for unit tests. A fake result is not a Windows measurement."""

    if api is None:
        raise PreflightError("lab_windows_api_unavailable")
    token = _CURRENT.set(api)
    try:
        yield api
    finally:
        _CURRENT.reset(token)


def current_windows_lab_api():
    api = _CURRENT.get()
    if api is not None:
        return api
    if sys.platform != "win32":
        raise PreflightError("lab_windows_api_unavailable")
    global _REAL
    if _REAL is None:
        _REAL = WindowsLabApi()
    return _REAL


class WindowsLabApi:
    """Real WinAPI adapter. Methods refuse before WinDLL when not running on Windows."""

    def open_process(self, pid: int):
        self._windows_only()

        def opener(access, inherit, value):
            from ctypes import wintypes

            from inferyard.platforms.windows_api import _error, _function, _kernel

            function = _function(
                _kernel(),
                "OpenProcess",
                [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD],
                wintypes.HANDLE,
            )
            handle = function(access, inherit, value)
            if not handle:
                raise _error()
            return handle

        return _bounded_open_process(opener, pid)

    def close_handle(self, handle) -> None:
        self._windows_only()
        from ctypes import wintypes

        from inferyard.platforms.windows_api import _error, _function, _kernel

        close = _function(_kernel(), "CloseHandle", [wintypes.HANDLE])
        if not close(handle):
            raise _error()

    def creation_filetime(self, handle) -> int:
        self._windows_only()
        import ctypes
        from ctypes import wintypes

        from inferyard.platforms.windows_api import _error, _function, _kernel

        get_times = _function(
            _kernel(),
            "GetProcessTimes",
            [wintypes.HANDLE] + [ctypes.POINTER(wintypes.FILETIME)] * 4,
        )
        values = [wintypes.FILETIME() for _ in range(4)]
        if not get_times(handle, *(ctypes.byref(value) for value in values)):
            raise _error()
        return (values[0].dwHighDateTime << 32) | values[0].dwLowDateTime

    def wait_result(self, handle) -> int:
        self._windows_only()
        from ctypes import wintypes

        from inferyard.platforms.windows_api import _error, _function, _kernel

        wait = _function(
            _kernel(),
            "WaitForSingleObject",
            [wintypes.HANDLE, wintypes.DWORD],
            wintypes.DWORD,
        )
        result = wait(handle, 0)
        if result == WAIT_FAILED:
            raise _error()
        return int(result)

    def current_account(self) -> str:
        return self._username(None)

    def process_account(self, pid: int) -> str:
        return self._username(pid)

    def process_executable(self, pid: int) -> str:
        return self._process_attr(pid, "exe")

    def process_argv(self, pid: int) -> list[str]:
        return self._process_attr(pid, "cmdline")

    def process_cwd(self, pid: int) -> str:
        self._windows_only()
        import psutil

        try:
            resolved = psutil.Process(pid).cwd()
            from pathlib import Path

            return str(Path(resolved).resolve(strict=True))
        except (OSError, psutil.Error) as exc:
            raise PreflightError("lab_windows_identity_incomplete") from exc

    def tcp_listeners(self) -> list[tuple[str, int, int | None]]:
        self._windows_only()
        import psutil

        try:
            connections = psutil.net_connections(kind="tcp")
        except (OSError, psutil.Error) as exc:
            raise PreflightError("lab_windows_identity_incomplete") from exc
        return [
            (row.laddr.ip, row.laddr.port, row.pid)
            for row in connections
            if row.status == psutil.CONN_LISTEN
        ]

    def executable_sha256(self, path: str) -> str:
        """Reuse the existing Windows file hash. It has no per-chunk deadline."""

        self._windows_only()
        from inferyard.platforms.engine_fit_windows_files import file_hash

        try:
            digest, _size = file_hash(path)
        except (OSError, PreflightError) as exc:
            raise PreflightError("lab_windows_executable_unreadable") from exc
        if type(digest) is not str:
            raise PreflightError("lab_windows_executable_unreadable")
        return digest

    def reject_reparse(self, path: str) -> None:
        self._windows_only()
        import os
        from pathlib import Path

        from inferyard.platforms.windows_api import open_file

        descriptor = open_file(Path(path), os.O_RDONLY)
        os.close(descriptor)

    def path_stamp(self, path: str):
        self._windows_only()
        from inferyard.platforms.engine_fit_windows_files import path_stamp

        return path_stamp(path)

    def open_read(self, path: str):
        self._windows_only()
        import os
        from pathlib import Path

        from inferyard.platforms.windows_api import open_file

        return open_file(Path(path), os.O_RDONLY)

    def descriptor_stamp(self, handle):
        self._windows_only()
        from inferyard.platforms.engine_fit_windows_files import descriptor_stamp

        return descriptor_stamp(handle)

    def read_file(self, handle, size: int) -> bytes:
        self._windows_only()
        import os

        return os.read(handle, size)

    def close_file(self, handle) -> None:
        self._windows_only()
        import os

        os.close(handle)

    def _username(self, pid: int | None) -> str:
        self._windows_only()
        import psutil

        try:
            process = psutil.Process() if pid is None else psutil.Process(pid)
            return process.username()
        except (OSError, psutil.Error) as exc:
            raise PreflightError("lab_windows_identity_incomplete") from exc

    def _process_attr(self, pid: int, name: str):
        self._windows_only()
        import psutil

        try:
            return getattr(psutil.Process(pid), name)()
        except (OSError, psutil.Error) as exc:
            raise PreflightError("lab_windows_identity_incomplete") from exc

    @staticmethod
    def _windows_only() -> None:
        if sys.platform != "win32":
            raise PreflightError("lab_windows_api_unavailable")
