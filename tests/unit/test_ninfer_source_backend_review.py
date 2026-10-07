"""Codex 首轮审查回归。注入 API，不调用真实 WinAPI。"""

import ctypes
from ctypes import wintypes

import pytest

from scripts.ninfer_source_host.custody import BackendError, BackendStartError
from scripts.ninfer_source_host.windows_backend import WindowsBackend
from scripts.ninfer_source_host.windows_backend_limits import PUMP_MAX, RETAIN_MAX
from scripts.ninfer_source_host.windows_backend_winapi import RealWindowsApi, _basic_limits
from tests.unit.test_ninfer_source_backend import spec

HIGH = 0x1234567887654321
_API_NAMES = (
    "CreateProcessW",
    "CreatePipe",
    "SetHandleInformation",
    "CloseHandle",
    "CreateJobObjectW",
    "SetInformationJobObject",
    "AssignProcessToJobObject",
    "ResumeThread",
    "PeekNamedPipe",
    "ReadFile",
    "WaitForSingleObject",
    "GetExitCodeProcess",
    "TerminateProcess",
)


class _ProcessInfo(ctypes.Structure):
    _fields_ = [("hProcess", ctypes.c_void_p), ("hThread", ctypes.c_void_p)]


class _ParentKernel:
    def __init__(self):
        self.closes = []

    def CreateProcessW(self, *_args):
        info = _args[-1]._obj
        info.hProcess = 201
        info.hThread = 202
        return 1

    def CloseHandle(self, handle):
        self.closes.append(handle)
        return 0 if self.closes == [101] else 1


class _JobKernel:
    def __init__(self):
        self.closes = []

    def CreateJobObjectW(self, *_args):
        return HIGH

    def SetInformationJobObject(self, *_args):
        return 0

    def CloseHandle(self, handle):
        self.closes.append(handle)
        return 0 if handle == HIGH else 1


class _Function:
    def __init__(self):
        self.argtypes = None
        self.restype = ctypes.c_int


def _raw(kernel):
    raw = RealWindowsApi.__new__(RealWindowsApi)
    raw._win32 = lambda: None
    raw._kernel = kernel
    raw._dll = lambda: (kernel, ctypes, wintypes)
    return raw


def test_failed_parent_close_stays_unknown_and_is_not_closed_again():
    kernel = _ParentKernel()
    raw = _raw(kernel)
    raw._pipes = lambda *_args: (101, 102, 103, 104, 105, 106)
    raw._clear_inherit = lambda *_args: None
    raw._startup = lambda *_args: ctypes.c_int()
    raw._process_info = lambda *_args: _ProcessInfo()
    host = WindowsBackend(api=raw)
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    ref = caught.value.ref
    assert ref.process == 201
    assert ref.thread == 202
    assert ref.unknown == (101,)
    assert ref.extra == (102, 104, 106)
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert kernel.closes == [101, 201, 202, 102, 104, 106]
    assert ref.unknown == (101,)
    assert ref.process == 201
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert kernel.closes == [101, 201, 202, 102, 104, 106]
    assert ref.unknown == (101,)


def test_create_job_close_failure_keeps_full_handle_unknown():
    kernel = _JobKernel()
    raw = _raw(kernel)

    class Spawned:
        process = 201
        thread = 202
        stdout = 1
        stderr = 2

    raw.spawn_suspended = lambda *_args: Spawned()
    host = WindowsBackend(api=raw)
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    ref = caught.value.ref
    assert ref.job is None
    assert ref.unknown == (HIGH,)
    assert ref.process == 201
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert kernel.closes == [HIGH, 201, 202]
    assert ref.unknown == (HIGH,)
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert kernel.closes == [HIGH, 201, 202]


def test_adapter_prototypes_keep_high_handles_and_job_layout(monkeypatch):
    dll = type("Dll", (), {})()
    for name in _API_NAMES:
        setattr(dll, name, _Function())
    raw = RealWindowsApi.__new__(RealWindowsApi)
    raw._kernel = None
    raw._win32 = lambda: None
    monkeypatch.setattr(ctypes, "WinDLL", lambda *_args, **_kwargs: dll, raising=False)
    loaded, *_rest = raw._dll()
    for name in _API_NAMES:
        function = getattr(loaded, name)
        assert function.argtypes is not None
        assert function.restype is not None
    job = loaded.CreateJobObjectW
    close = loaded.CloseHandle
    assign = loaded.AssignProcessToJobObject
    resume = loaded.ResumeThread
    wait = loaded.WaitForSingleObject
    assert job.restype is ctypes.c_void_p
    assert close.argtypes == [ctypes.c_void_p]
    assert assign.argtypes == [ctypes.c_void_p, ctypes.c_void_p]
    assert resume.restype is ctypes.c_uint32
    assert wait.restype is ctypes.c_uint32
    for name in (
        "SetHandleInformation",
        "SetInformationJobObject",
        "PeekNamedPipe",
        "ReadFile",
        "GetExitCodeProcess",
        "TerminateProcess",
    ):
        assert getattr(loaded, name).argtypes[0] is ctypes.c_void_p
    produced = ctypes.CFUNCTYPE(job.restype, *job.argtypes)(lambda *_args: HIGH)
    assert produced(None, None) == HIGH
    seen = []

    def capture(value):
        seen.append(value)
        return 1

    assert ctypes.CFUNCTYPE(close.restype, *close.argtypes)(capture)(HIGH) == 1
    assert seen == [HIGH]

    def capture_two(job_handle, process_handle):
        seen.extend((job_handle, process_handle))
        return 1

    seen.clear()
    caller = ctypes.CFUNCTYPE(assign.restype, *assign.argtypes)(capture_two)
    assert caller(HIGH, HIGH) == 1
    assert seen == [HIGH, HIGH]
    failed = ctypes.CFUNCTYPE(resume.restype, *resume.argtypes)(lambda _handle: 0xFFFFFFFF)
    assert failed(HIGH) == 0xFFFFFFFF
    callback = ctypes.CFUNCTYPE(ctypes.c_void_p)(lambda: HIGH)
    address = ctypes.cast(callback, ctypes.c_void_p).value
    truncated = ctypes.CFUNCTYPE(ctypes.c_int)(address)()
    assert truncated == -2023406815
    assert truncated != HIGH
    info = _basic_limits(ctypes)
    kind = type(info)
    assert ctypes.sizeof(info) == 64
    assert kind.LimitFlags.offset == 16
    assert kind._pad_flags.offset == 20
    assert kind.MinimumWorkingSetSize.offset == 24
    assert kind.MaximumWorkingSetSize.offset == 32
    assert kind.ActiveProcessLimit.offset == 40
    assert kind._pad_limit.offset == 44
    assert kind.Affinity.offset == 48
    assert kind.PriorityClass.offset == 56
    assert kind.SchedulingClass.offset == 60
    assert kind._pad_flags.offset - kind.LimitFlags.offset == 4
    assert kind.PriorityClass.offset - kind.ActiveProcessLimit.offset == 16


class _Streams:
    def __init__(self, *, fail):
        self.stdout = b"abc"
        self.fail = fail
        self.reads = []
        self.peeked = []

    def peek(self, pipe):
        self.peeked.append(pipe)
        if pipe == "stderr" and self.fail == "peek":
            self.fail = None
            raise OSError("stderr_peek")
        if pipe == "stdout":
            return len(self.stdout), not self.stdout
        if self.fail == "read":
            return 4, False
        return 0, True

    def read(self, pipe, size):
        assert 0 < size <= PUMP_MAX
        self.reads.append((pipe, size))
        if pipe == "stderr" and self.fail == "read":
            self.fail = None
            raise OSError("stderr_read")
        assert pipe == "stdout"
        chunk = self.stdout[:size]
        self.stdout = self.stdout[size:]
        return chunk


def _deliver_once(fail):
    from scripts.ninfer_source_host.windows_backend import ProcessRef

    api = _Streams(fail=fail)
    host = WindowsBackend(api=api)
    ref = ProcessRef(object(), object(), "stdout", "stderr")
    with pytest.raises(BackendError):
        host.pump(ref, PUMP_MAX)
    assert ref.held_stdout == b"abc"
    assert host.pump(ref, PUMP_MAX) == {"stdout": b"abc", "stderr": b"", "eof": True}
    assert ref.held_stdout == b""
    again = host.pump(ref, PUMP_MAX)
    assert again == {"stdout": b"", "stderr": b"", "eof": True}
    assert b"abc" not in again["stdout"]
    assert all(size <= PUMP_MAX for _pipe, size in api.reads)
    return api


def test_stderr_peek_failure_redelivers_stdout_once():
    api = _deliver_once("peek")
    assert api.reads == [("stdout", 3)]
    assert "stderr" not in [pipe for pipe, _size in api.reads]


def test_stderr_read_failure_redelivers_stdout_once():
    api = _deliver_once("read")
    assert ("stderr", 4) in api.reads
    assert api.reads[0] == ("stdout", 3)


def test_retained_output_stops_at_32768_and_reads_stay_at_4096():
    assert PUMP_MAX == 4096
    assert RETAIN_MAX == 32768

    class Grow:
        def __init__(self):
            self.stdout = b"z" * (PUMP_MAX * 9)
            self.reads = []

        def peek(self, pipe):
            if pipe == "stderr":
                raise OSError("stderr")
            return len(self.stdout), False

        def read(self, pipe, size):
            assert size <= PUMP_MAX
            self.reads.append(size)
            chunk = self.stdout[:size]
            self.stdout = self.stdout[size:]
            return chunk

    from scripts.ninfer_source_host.windows_backend import ProcessRef

    api = Grow()
    host = WindowsBackend(api=api)
    ref = ProcessRef(object(), object(), "stdout", "stderr")
    for _index in range(8):
        with pytest.raises(BackendError):
            host.pump(ref, PUMP_MAX)
    assert ref.held_stdout == b"z" * PUMP_MAX
    assert api.reads == [PUMP_MAX]
    assert api.stdout == b"z" * (PUMP_MAX * 8)
    for _index in range(3):
        with pytest.raises(BackendError):
            host.pump(ref, PUMP_MAX)
    assert api.reads == [PUMP_MAX]
    assert api.stdout == b"z" * (PUMP_MAX * 8)
    assert ref.held_stdout == b"z" * PUMP_MAX
    api.peek = lambda pipe: (len(api.stdout), not api.stdout) if pipe == "stdout" else (0, True)
    delivered = b""
    for _index in range(10):
        pumped = host.pump(ref, PUMP_MAX)
        delivered += pumped["stdout"]
        assert len(pumped["stdout"]) <= PUMP_MAX
    assert delivered == b"z" * (PUMP_MAX * 9)
    assert ref.held_stdout == b""
    assert api.stdout == b""
    assert api.reads == [PUMP_MAX] * 9
    done = host.pump(ref, PUMP_MAX)
    assert done == {"stdout": b"", "stderr": b"", "eof": True}
    assert api.reads == [PUMP_MAX] * 9
