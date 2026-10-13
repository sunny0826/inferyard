"""Stage46 第二轮正向回归。捕获真实 sink，并经公开 run_fixture 走到 NativeJobBoundary。"""

import ctypes
import io
import sys
from unittest.mock import patch

import pytest

import scripts.ninfer_source_host.probe_fixture_clock as clock_mod
import scripts.ninfer_source_host.probe_fixture_worker as worker
from scripts.ninfer_source_host.probe_fixture import run_fixture
from scripts.ninfer_source_host.probe_fixture_clock import ClockError
from scripts.ninfer_source_host.probe_fixture_custody import PipeHold
from scripts.ninfer_source_host.probe_fixture_descendant import DescendantHold
from scripts.ninfer_source_host.probe_fixture_frames import loads_frame
from scripts.ninfer_source_host.probe_io_limits import OUTPUT_PREFIX, STREAM_LIMIT
from scripts.ninfer_source_host.probe_io_winapi import RealProbeFileApi
from tests.unit.test_ninfer_source_file_io import Mem
from tests.unit.test_ninfer_source_probe_fixture import Clock, run
from tests.unit.test_ninfer_source_probe_io import HIGH, HOST, JOB, request

_WAIT_TIMEOUT = 258
_STILL_ACTIVE = 259


class Kernel:
    def __init__(self, *, fail_create=None, wait=0, exit_code=1, close_ok=True):
        self.fail_create = fail_create
        self.wait_code = wait
        self.exit_code = exit_code
        self.close_ok = close_ok
        self.calls = []
        self.closed = []

    def GetCurrentProcess(self):
        return 11

    def IsProcessInJob(self, process, job, assigned):
        self.calls.append(("job", process, job))
        assigned._obj.value = 1
        return 1

    def QueryInformationJobObject(self, job, kind, info, size, returned):
        self.calls.append(("query", kind, job))
        if kind == 9:
            info._obj.BasicLimitInformation.LimitFlags = 0x8
            info._obj.BasicLimitInformation.ActiveProcessLimit = 1
        if kind == 1:
            info._obj.TotalTerminatedProcesses = 99
        return 1

    def CreateProcessW(self, *args):
        self.calls.append(("create", args[0], args[4], args[5]))
        if self.fail_create is not None:
            return 0
        info = args[-1]._obj
        info.hProcess = HIGH
        info.hThread = HIGH + 1
        return 1

    def AssignProcessToJobObject(self, job, process):
        self.calls.append(("assign", job, process))
        return 1

    def ResumeThread(self, thread):
        self.calls.append(("resume", thread))
        return 1

    def WaitForSingleObject(self, handle, timeout):
        self.calls.append(("wait", handle, timeout))
        return self.wait_code

    def GetExitCodeProcess(self, process, code):
        self.calls.append(("exit", process))
        code._obj.value = self.exit_code
        return 1

    def TerminateProcess(self, process, code):
        self.calls.append(("terminate", process, code))
        return 1

    def CloseHandle(self, handle):
        self.closed.append(handle)
        return 1 if self.close_ok else 0


def _api(kernel):
    raw = RealProbeFileApi.__new__(RealProbeFileApi)
    raw._win32 = lambda: None
    raw._dll = lambda: (kernel, ctypes)
    return raw


def _errors(monkeypatch, code=5):
    monkeypatch.setattr(ctypes, "get_last_error", lambda: code, raising=False)
    monkeypatch.setattr(ctypes, "WinError", lambda value: OSError(value), raising=False)


def _last(raw):
    lines = [line for line in raw.splitlines() if line.startswith(b"{")]
    assert lines
    return loads_frame(lines[-1])


def test_file_io_publishes_bound_frame_on_the_sink():
    body = request("read")
    code, sink = run("file_io", body, io_api=Mem({body["path"]: b"hi"}))
    raw = sink.getvalue()
    frame = loads_frame(raw)
    assert code == 0
    assert raw.endswith(b"\n")
    assert frame["status"] == "ok"
    assert frame["host_id"] == HOST
    assert frame["job_id"] == JOB
    assert frame["result"]["primary_error"] is None
    assert frame["result"]["cleanup_errors"] == []


def test_file_io_publish_failure_is_not_success():
    body = request("read")

    class Sink:
        def write(self, data):
            raise OSError("publish")

        def flush(self):
            raise AssertionError("flush after failed write")

        def getvalue(self):
            return b""

    code, sink = run("file_io", body, io_api=Mem({body["path"]: b"hi"}), stdout=Sink())
    assert code == 2
    assert sink.getvalue() == b""


def test_encoding_past_total_replaces_ok_and_returns_nonzero():
    clock = Clock()
    body = request("read")
    original = worker.result_frame

    def slow_frame(*args, **kwargs):
        value = original(*args, **kwargs)
        clock.t = 100
        return value

    with patch.object(worker, "result_frame", slow_frame):
        code, sink = run("file_io", body, io_api=Mem({body["path"]: b"hi"}), now_ticks=clock)
    # 编码把时钟推进到原 total 之后。失败事实只留在内存，不能再写一帧。
    assert code == 2
    assert clock.t == 100
    assert sink.getvalue() == b""
    frame = loads_frame(sink.frame.payload.strip())
    assert frame["status"] == "error"
    assert frame["result"]["primary_error"] == "io_deadline"


def test_ok_frame_flushed_after_total_does_not_stay_successful():
    clock = Clock()
    body = request("read")

    class LateFlush(io.BytesIO):
        def flush(self):
            self.clock_hits = getattr(self, "clock_hits", 0) + 1
            if self.clock_hits == 1:
                clock.t = 100
            super().flush()

    code, sink = run(
        "file_io", body, io_api=Mem({body["path"]: b"hi"}), now_ticks=clock, stdout=LateFlush()
    )
    # 复审：原 total 之后不再开始发布、flush 或关闭。已写出的 ok 帧不能再补一帧更正。
    lines = [line for line in sink.getvalue().splitlines() if line.startswith(b"{")]
    assert code == 2
    assert len(lines) == 1
    assert loads_frame(lines[0])["status"] == "ok"
    assert clock.t == 100


def test_regressed_clock_vetoes_success_without_another_open():
    source = Clock()
    body = request("read")
    opens = []
    closes = []

    class Api:
        def is_reparse(self, path):
            return False

        def open_read(self, path):
            opens.append(path)
            return 7

        def read(self, handle, size):
            return b"hi" if size > 1 else b""

        def flush(self, handle):
            return None

        def fsync(self, handle):
            return None

        def close(self, handle):
            closes.append(handle)
            source.t = 0

    sink = io.BytesIO()
    with pytest.raises(ClockError, match="fixture_clock_regression") as caught:
        run("file_io", body, io_api=Api(), now_ticks=source, stdout=sink)
    assert sink.getvalue() == b""
    # 关闭已经成功，随后的倒退永久停用。不再发布，也不再关第二次。
    error = caught.value
    assert opens == [body["path"]]
    assert closes == [7]
    opened = error.resources.handles[0]
    assert opened.value == 7
    assert opened.closed is True
    assert opened.unknown is False
    assert error.io_result["bytes"] == 2


def test_output_flush_past_total_is_nonzero_and_raw():
    clock = Clock()

    class LateFlush(io.BytesIO):
        def flush(self):
            clock.t = 100
            super().flush()

    code, sink = run("output", request("hash"), now_ticks=clock, stdout=LateFlush())
    raw = sink.getvalue()
    assert code == 2
    assert clock.t == 100
    assert raw.startswith(OUTPUT_PREFIX)
    assert len(raw) > STREAM_LIMIT
    assert not raw.startswith(b"{")
    assert b"data_b64" not in raw


@pytest.mark.parametrize("kind", ["zero", "partial", "raise", "flush"])
def test_output_short_partial_and_flush_failures_are_nonzero(kind):
    class Sink:
        def __init__(self):
            self.buf = bytearray()

        def write(self, data):
            if kind == "raise":
                raise OSError("write")
            if kind == "zero":
                return 0
            if kind == "partial":
                self.buf.extend(data[:4])
                return 4
            self.buf.extend(data)
            return len(data)

        def flush(self):
            if kind == "flush":
                raise OSError("flush")

        def getvalue(self):
            return bytes(self.buf)

    sink = Sink()
    code, wrapped = run("output", request("hash"), stdout=sink)
    raw = wrapped.getvalue()
    assert code == 2
    assert not raw.startswith(b"{")
    if kind == "zero":
        assert raw == b""
    if kind == "partial":
        assert len(raw) == 4
    if kind == "flush":
        assert raw.startswith(OUTPUT_PREFIX)
        assert len(raw) > STREAM_LIMIT


def test_blocked_read_past_total_keeps_the_fact_and_does_not_close():
    clock = Clock()

    class Pipe:
        injected = True

        def __init__(self):
            self.closed = []
            self.order = []

        def create_pipe(self):
            return 101, 102

        def read(self, handle, count):
            clock.t = 100
            return b"x"

        def close(self, handle):
            self.closed.append(handle)

    pipe = Pipe()
    sink = io.BytesIO()
    with pytest.raises(PipeHold) as caught:
        run("blocked_read", request("read"), native_api=pipe, now_ticks=clock, stdout=sink)
    hold = caught.value
    assert clock.t == 100
    assert pipe.closed == []
    assert hold.native is pipe
    assert hold.resources.native is pipe
    assert hold.detail["bytes"] == 1
    assert hold.detail["primary_error"] == "fixture_deadline"
    assert sink.getvalue().count(b"\n") == 1


def test_public_run_fixture_reaches_create_process_without_assign(monkeypatch):
    kernel = Kernel(exit_code=1)
    _errors(monkeypatch)

    def ticks():
        return 1

    monkeypatch.setattr(clock_mod, "qpc_ticks", ticks)
    monkeypatch.setattr(clock_mod, "qpc_frequency", lambda: 1)
    stdout = io.BytesIO()

    class Stdout:
        buffer = stdout

    monkeypatch.setattr(sys, "stdout", Stdout())
    code = run_fixture("descendant", request("hash"), native_api=_api(kernel))
    frame = _last(stdout.getvalue())
    assert code == 0
    assert frame["status"] == "job_terminated"
    assert frame["result"]["detail"]["assigned"] is False
    assert frame["result"]["detail"]["resumed"] is False
    assert frame["result"]["detail"]["exit_code"] == 1
    assert frame["host_id"] == HOST
    assert frame["job_id"] == JOB
    created = [call for call in kernel.calls if call[0] == "create"]
    assert created and created[0][1] == sys.executable
    assert created[0][2] is False
    assert created[0][3] == 0x4
    assert not any(call[0] in {"assign", "resume", "terminate"} for call in kernel.calls)
    assert not any(call[0] == "query" and call[1] == 1 for call in kernel.calls)
    assert kernel.closed == [HIGH + 1, HIGH]


def test_create_process_refusal_records_the_api_error_and_does_not_terminate(monkeypatch):
    kernel = Kernel(fail_create=5)
    _errors(monkeypatch, 5)
    code, sink = run("descendant", request("hash"), native_api=_api(kernel))
    frame = _last(sink.getvalue())
    assert code == 0
    assert frame["status"] == "creation_rejected"
    assert frame["result"]["detail"]["api"] == "CreateProcessW"
    assert frame["result"]["detail"]["winerror"] == 5
    assert frame["result"]["detail"]["observed"] == "create_process_refused"
    assert kernel.closed == []
    assert not any(call[0] in {"assign", "resume", "terminate", "wait"} for call in kernel.calls)


def test_normal_exit_zero_is_not_job_termination(monkeypatch):
    kernel = Kernel(exit_code=0)
    _errors(monkeypatch)
    code, sink = run("descendant", request("hash"), native_api=_api(kernel))
    frame = _last(sink.getvalue())
    assert code == 2
    assert frame["status"] == "error"
    assert frame["result"]["classification"] == "normal_exit"
    assert frame["result"]["detail"]["observed"] == "normal_exit_not_job_termination"
    assert not any(call[0] == "query" and call[1] == 1 for call in kernel.calls)


def test_unconfirmed_child_keeps_original_handles(monkeypatch):
    kernel = Kernel(wait=_WAIT_TIMEOUT, exit_code=_STILL_ACTIVE)
    _errors(monkeypatch)
    raw = _api(kernel)
    with pytest.raises(DescendantHold) as caught:
        run("descendant", request("hash"), native_api=raw)
    hold = caught.value
    assert kernel.closed == []
    assert hold.native is raw
    assert hold.resources is raw.retained
    assert hold.resources.process == HIGH
    assert hold.resources.thread == HIGH + 1
    assert hold.detail["wait"] == _WAIT_TIMEOUT
    assert hold.detail["exit_code"] == _STILL_ACTIVE
    assert not any(call[0] in {"assign", "resume", "terminate"} for call in kernel.calls)


def test_close_failure_is_recorded_after_the_attempt(monkeypatch):
    kernel = Kernel(exit_code=1, close_ok=False)
    _errors(monkeypatch, 6)
    raw = _api(kernel)
    with pytest.raises(DescendantHold) as caught:
        run("descendant", request("hash"), native_api=raw)
    hold = caught.value
    assert isinstance(hold.__cause__, OSError)
    assert kernel.closed == [HIGH + 1, HIGH]
    assert kernel.closed.count(HIGH) == 1
    assert kernel.closed.count(HIGH + 1) == 1
    assert hold.detail["handles"]["unknown"] == ["thread", "process"]
    assert hold.native is raw
    assert hold.resources is raw.retained
    assert hold.__cause__.resources is hold.resources
    before = list(kernel.closed)
    hold.resources.close_once(raw)
    assert kernel.closed == before
