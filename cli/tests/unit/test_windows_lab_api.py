"""Import and access-mask guards. These tests must not call WinDLL."""

import ast
import ctypes
import sys
from pathlib import Path

import pytest

from inferyard.platforms.identity import PreflightError
from inferyard.platforms.windows_lab_api import (
    PROCESS_ACCESS,
    WindowsLabApi,
    _bounded_open_process,
)
from inferyard.platforms.windows_lab_process import ProcessHandle

ROOT = Path(__file__).parents[2] / "src" / "inferyard" / "platforms"


def test_process_access_is_query_and_synchronize_only():
    terminate = 0x0001
    write = 0x0020 | 0x0008 | 0x0200
    query_information = 0x0400
    assert PROCESS_ACCESS == 0x00101000
    assert PROCESS_ACCESS & (terminate | write | query_information) == 0
    seen = {}

    def opener(access, inherit, pid):
        seen.update(access=access, inherit=inherit, pid=pid)
        return 7

    assert _bounded_open_process(opener, 42) == 7
    assert seen == {"access": PROCESS_ACCESS, "inherit": False, "pid": 42}


def test_sources_do_not_import_winreg_or_legacy_pid_checks():
    for path in sorted(ROOT.glob("windows_lab_*.py")):
        source = path.read_text(encoding="utf-8")
        tree = ast.parse(source)
        assert "import winreg" not in source
        assert "pid_exists" not in source
        assert "create_time" not in source
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                assert all(alias.name != "winreg" for alias in node.names)
            if isinstance(node, ast.ImportFrom):
                assert node.module != "winreg"
            if isinstance(node, ast.Call) and not _inside_function(node, tree):
                name = getattr(node.func, "attr", "")
                assert name != "WinDLL"


def _inside_function(node, tree):
    parents = {}
    for parent in ast.walk(tree):
        for child in ast.iter_child_nodes(parent):
            parents[child] = parent
    current = node
    while current in parents:
        current = parents[current]
        if isinstance(current, ast.FunctionDef | ast.AsyncFunctionDef):
            return True
    return False


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
