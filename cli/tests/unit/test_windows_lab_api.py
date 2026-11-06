"""Windows inspection must avoid mutating process permissions and unsupported APIs."""

import ctypes
import sys

import pytest

from inferyard.platforms.identity import PreflightError
from inferyard.platforms.windows_lab_api import WindowsLabApi, _bounded_open_process
from inferyard.platforms.windows_lab_process import ProcessHandle


def test_process_access_is_query_and_synchronize_only():
    seen = {}

    def opener(access, inherit, pid):
        seen.update(access=access, inherit=inherit, pid=pid)
        return 7

    assert _bounded_open_process(opener, 42) == 7
    terminate_or_write = 0x0001 | 0x0020 | 0x0008 | 0x0200
    assert seen["access"] & terminate_or_write == 0
    assert seen["inherit"] is False


@pytest.mark.skipif(sys.platform == "win32", reason="live WinAPI is outside this unit")
@pytest.mark.parametrize(
    ("method", "args"),
    [
        ("open_process", (4,)),
        ("close_handle", (1,)),
        ("creation_filetime", (1,)),
        ("wait_result", (1,)),
        ("current_account", ()),
        ("process_executable", (4,)),
        ("process_argv", (4,)),
        ("process_cwd", (4,)),
        ("tcp_listeners", ()),
        ("executable_sha256", (r"C:\engine\server.exe",)),
        ("reject_reparse", (r"C:\models\model.bin",)),
        ("path_stamp", (r"C:\models\model.bin",)),
        ("open_read", (r"C:\models\model.bin",)),
        ("read_file", (1, 1)),
        ("close_file", (1,)),
    ],
)
def test_real_api_refuses_off_windows_before_windll(monkeypatch, method, args):
    calls = []
    monkeypatch.setattr(ctypes, "WinDLL", lambda *_args, **_kwargs: calls.append(1), raising=False)
    with pytest.raises(PreflightError, match="lab_windows_api_unavailable"):
        getattr(WindowsLabApi(), method)(*args)
    assert calls == []
    with pytest.raises(PreflightError, match="lab_windows_api_unavailable"), ProcessHandle(4):
        pass
    assert calls == []
