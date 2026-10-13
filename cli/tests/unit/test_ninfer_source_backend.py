"""Windows Backend 注入回归。fake API 不构成原生 Job、管道或句柄证据。"""

import ctypes
import os
import sys

import psutil
import pytest

from scripts.ninfer_source_host.windows_backend import WindowsBackend
from scripts.ninfer_source_host.windows_backend_limits import (
    ACTIVE_PROCESS_LIMIT,
    BREAKAWAY_FLAGS,
    CREATE_BREAKAWAY_FROM_JOB,
    CREATE_SUSPENDED,
    JOB_LIMIT_FLAGS,
)
from scripts.ninfer_source_host.windows_backend_winapi import (
    RealWindowsApi,
    _command_line,
    basic_limits_nbytes,
)

SHA = "ab" * 32
OTHER = "cd" * 32
ROOT = __import__("pathlib").Path(__file__).resolve().parents[2]


class HostError(Exception):
    def __init__(self, code):
        super().__init__(code)
        self.code = code


class BackendError(HostError):
    def __init__(self, code="host_backend"):
        super().__init__(code)


class BackendStartError(BackendError):
    def __init__(self, ref, cause):
        super().__init__()
        self.ref = ref
        self.cause = cause


ERRS = (HostError, BackendError, BackendStartError)


class Pipe:
    def __init__(self, data=b"", eof=False):
        self.data = data
        self.eof = eof


class FakeApi:
    def __init__(self, fail=None):
        self.fail = fail
        self.order = []
        self.process = object()
        self.thread = object()
        self.stdout = Pipe()
        self.stderr = Pipe()
        self.job = object()
        self.flags = None
        self.active = None
        self.creation = None
        self.assigned = None
        self.resumed = None
        self.read_sizes = []
        self.peeked = []
        self.wait_result = 0x102
        self.wait_raises = False
        self.exit_value = 0
        self.exit_raises = False
        self.exit_process = None
        self.waited = None
        self.closed = []
        self.close_raises = False
        self.terminated = []
        self.killed = []

    def spawn_suspended(self, argv, cwd, creation_flags):
        self.order.append("spawn")
        self.creation = creation_flags
        self.argv = argv
        self.cwd = cwd
        if self.fail == "spawn":
            raise OSError("spawn")
        if self.fail == "partial":
            error = OSError("partial")
            error.spawned = Pipe()
            error.spawned.process = self.process
            error.spawned.thread = self.thread
            error.spawned.stdout = self.stdout
            error.spawned.stderr = self.stderr
            error.extra = (object(),)
            raise error
        return _spawned(self)

    def create_job(self, flags, active_limit):
        self.order.append("job")
        self.flags = flags
        self.active = active_limit
        if self.fail == "job":
            raise OSError("job")
        if self.fail == "job-leak":
            error = OSError("job")
            error.leaked_job = self.job
            raise error
        return self.job

    def assign_job(self, job, process):
        self.order.append("assign")
        self.assigned = (job, process)
        if self.fail == "assign":
            raise OSError("assign")

    def resume(self, thread):
        self.order.append("resume")
        self.resumed = thread
        if self.fail == "resume":
            raise OSError("resume")

    def peek(self, pipe):
        self.peeked.append(pipe)
        return len(pipe.data), pipe.eof

    def read(self, pipe, size):
        self.read_sizes.append(size)
        chunk = pipe.data[:size]
        pipe.data = pipe.data[size:]
        return chunk

    def wait_zero(self, process):
        self.waited = process
        if self.wait_raises:
            raise OSError("wait")
        return self.wait_result

    def get_exit_code(self, process):
        self.exit_process = process
        if self.exit_raises:
            raise OSError("exit")
        return self.exit_value

    def terminate(self, process):
        self.terminated.append(process)

    def kill(self, process):
        self.killed.append(process)

    def close(self, handle):
        self.closed.append(handle)
        if self.close_raises:
            raise OSError("close")


def _spawned(api):
    spawned = Pipe()
    spawned.process = api.process
    spawned.thread = api.thread
    spawned.stdout = api.stdout
    spawned.stderr = api.stderr
    return spawned


def spec(**over):
    base = {
        "argv": ("C:\\lab\\tool.exe", "--once"),
        "cwd": "C:\\lab",
        "executable_sha256": SHA,
        "script_sha256": OTHER,
        "no_descendants": True,
    }
    base.update(over)
    return base


def backend(fail=None, *, errors=ERRS):
    api = FakeApi(fail)
    return api, WindowsBackend(api, errors=errors)


def _is_project_cli(argv):
    # The Python executable itself can live under an inferyard checkout.
    executable_names = {part.replace("\\", "/").rsplit("/", 1)[-1] for part in argv}
    return bool(executable_names & {"inferyard", "inferyard.exe"}) or any(
        flag == "-m" and (module == "inferyard" or module.startswith("inferyard."))
        for flag, module in zip(argv, argv[1:], strict=False)
    )


@pytest.mark.parametrize(
    ("argv", "expected"),
    [
        (["/work/inferyard/.venv/bin/python", "-c", "resource_tracker"], False),
        ([r"C:\work\inferyard\.venv\Scripts\python.exe", "-c", "resource_tracker"], False),
        (["/work/inferyard/.venv/bin/inferyard", "run"], True),
        ([r"C:\work\inferyard\.venv\Scripts\inferyard.exe", "run"], True),
        (["python", "-I", "-m", "inferyard", "run"], True),
        (["python", "-m", "inferyard.cli", "run"], True),
    ],
)
def test_project_cli_detection_uses_command_tokens(argv, expected):
    assert _is_project_cli(argv) is expected


def test_pytest_process_is_not_an_asset_worker():
    assert "pytest" in " ".join(sys.argv)
    assert os.environ.get("RUN_STAGE") in (None, "")
    for child in psutil.Process().children(recursive=True):
        argv = child.cmdline()
        assert "run_windows" not in " ".join(argv)
        assert not _is_project_cli(argv)


def test_job_limits_forbid_breakaway_and_cap_one_process():
    assert CREATE_SUSPENDED == 0x4
    assert CREATE_SUSPENDED & CREATE_BREAKAWAY_FROM_JOB == 0
    assert JOB_LIMIT_FLAGS == 0x8
    assert ACTIVE_PROCESS_LIMIT == 1
    assert JOB_LIMIT_FLAGS & BREAKAWAY_FLAGS == 0
    if ctypes.sizeof(ctypes.c_void_p) == 8:
        assert basic_limits_nbytes() == 64


def test_command_line_quotes_without_loading_winapi():
    assert _command_line(("C:\\lab\\tool.exe", "--once")) == "C:\\lab\\tool.exe --once"
    assert _command_line(("a b", "")) == '"a b" ""'
    assert _command_line(('a"b\\',)) == '"a\\"b\\\\"'


@pytest.mark.skipif(sys.platform == "win32", reason="refusal is the non-windows path")
def test_real_constructor_refuses_before_windll(monkeypatch):
    calls = []
    monkeypatch.setattr(ctypes, "WinDLL", lambda *args, **kwargs: calls.append(args), raising=False)
    with pytest.raises(BackendError) as caught:
        WindowsBackend(errors=ERRS)
    assert caught.value.code == "host_backend"
    with pytest.raises(OSError, match="winapi_unavailable"):
        RealWindowsApi()
    raw = RealWindowsApi.__new__(RealWindowsApi)
    with pytest.raises(OSError, match="winapi_unavailable"):
        raw.peek(object())
    with pytest.raises(OSError, match="winapi_unavailable"):
        raw.spawn_suspended(("a",), "C:\\lab", CREATE_SUSPENDED)
    assert calls == []


def test_real_custody_exception_on_construct_and_missing_process(monkeypatch):
    calls = []
    monkeypatch.setattr(ctypes, "WinDLL", lambda *args, **kwargs: calls.append(1), raising=False)
    from scripts.ninfer_source_host.custody import BackendError as RealBackendError
    from scripts.ninfer_source_host.custody import BackendStartError as RealStartError

    if sys.platform != "win32":
        with pytest.raises(RealBackendError) as caught:
            WindowsBackend()
        assert type(caught.value) is RealBackendError
        assert caught.value.code == "host_backend"
    api = FakeApi("spawn")
    host = WindowsBackend(api)
    with pytest.raises(RealStartError) as started:
        host.start(spec())
    assert started.value.ref is None
    assert isinstance(started.value.cause, OSError)
    assert api.order == ["spawn"]
    assert api.terminated == []
    assert calls == []


def test_fake_start_order_does_not_import_custody_or_spawn():
    before = "scripts.ninfer_source_host.custody" in sys.modules
    children = {child.pid for child in psutil.Process().children(recursive=True)}
    api, host = backend()
    ref = host.start(spec())
    assert ref.process is api.process
    assert ref.thread is api.thread
    assert ref.stdout is api.stdout
    assert api.order == ["spawn", "job", "assign", "resume"]
    assert api.creation == CREATE_SUSPENDED
    assert api.flags == JOB_LIMIT_FLAGS
    assert api.active == 1
    assert api.assigned == (api.job, api.process)
    assert api.resumed is api.thread
    assert ("scripts.ninfer_source_host.custody" in sys.modules) is before
    assert children == {child.pid for child in psutil.Process().children(recursive=True)}


@pytest.mark.parametrize(
    "over",
    [
        {"argv": ["C:\\lab\\tool.exe"]},
        {"argv": ()},
        {"argv": ("bad\0",)},
        {"argv": ("\ud800",)},
        {"argv": ("x" * 12001,)},
        {"cwd": "relative"},
        {"cwd": "C:relative"},
        {"cwd": "\\\\?\\C:\\lab"},
        {"cwd": "C:\\lab\\..\\x"},
        {"executable_sha256": "AB" * 32},
        {"executable_sha256": SHA + "\n"},
        {"no_descendants": False},
        {"no_descendants": 1},
        {"extra": 1},
    ],
)
def test_bad_spec_does_nothing(over):
    api, host = backend()
    with pytest.raises(HostError) as caught:
        host.start(spec(**over))
    assert caught.value.code == "host_job"
    assert api.order == []
    assert api.terminated == []
    assert api.closed == []


def test_utf16_boundary_is_allowed_and_spawns_once():
    api, host = backend()
    ref = host.start(spec(argv=("x" * 12000,)))
    assert ref.process is api.process
    assert api.order == ["spawn", "job", "assign", "resume"]
    assert api.argv == ("x" * 12000,)


def test_partial_spawn_keeps_original_ref_and_does_not_kill():
    api, host = backend("partial")
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    assert caught.value.ref.process is api.process
    assert caught.value.ref.thread is api.thread
    assert len(caught.value.ref.extra) == 1
    assert api.order == ["spawn"]
    assert api.terminated == []
    assert api.killed == []
    assert api.closed == []
