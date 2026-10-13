"""Stage46 工作期限和文件保管。task 内才开始下一次工作；原 total 之后不再收尾。"""

import gc
import io
import sys
import weakref

import pytest

import scripts.ninfer_source_host.probe_fixture as entry
import scripts.ninfer_source_host.probe_fixture_clock as clock_mod
import scripts.ninfer_source_host.probe_fixture_worker as worker
from scripts.ninfer_source_host.file_io_codec import IoError
from scripts.ninfer_source_host.probe_fixture import run_fixture
from scripts.ninfer_source_host.probe_fixture_clock import bind_request_clock
from scripts.ninfer_source_host.probe_fixture_custody import DeadlineStop
from scripts.ninfer_source_host.probe_fixture_deadline import Deadline
from scripts.ninfer_source_host.probe_fixture_descendant import try_descendant
from scripts.ninfer_source_host.probe_fixture_files import FileHold
from scripts.ninfer_source_host.probe_io import ProbeFileApi
from scripts.ninfer_source_host.probe_io_limits import FILE_ROOT
from scripts.ninfer_source_host.probe_io_native import NativeJobBoundary
from tests.unit.test_ninfer_source_file_io import Mem, request
from tests.unit.test_ninfer_source_probe_fixture import Clock, Pipe, run
from tests.unit.test_ninfer_source_probe_fixture_round2 import HIGH, Kernel, _api, _errors
from tests.unit.test_ninfer_source_probe_io import NOTE, Native

_CANCELS = (KeyboardInterrupt, SystemExit)


def test_ready_flush_past_task_does_not_start_read():
    clock = Clock()

    class LateReady(io.BytesIO):
        def flush(self):
            clock.t = 51
            super().flush()

    pipe = Pipe()
    code, sink = run(
        "blocked_read", request("read"), native_api=pipe, now_ticks=clock, stdout=LateReady()
    )
    assert code == 2
    assert clock.t == 51
    assert "ready" not in pipe.order
    assert not any(item[0] == "read" for item in pipe.order if type(item) is tuple)
    assert pipe.closed == [102, 101]
    assert sink.getvalue().count(b"\n") >= 1


def test_output_payload_past_total_does_not_start_write():
    clock = Clock()
    original = worker._payload

    def late():
        value = original()
        clock.t = 100
        return value

    sink = io.BytesIO()
    with pytest.MonkeyPatch.context() as patch:
        patch.setattr(worker, "_payload", late)
        code, _returned = run("output", request("hash"), now_ticks=clock, stdout=sink)
    assert code == 2
    assert sink.getvalue() == b""
    assert clock.t == 100


@pytest.mark.parametrize("tick", [51, 100])
def test_final_binding_samples_after_the_decision(tick):
    clock = Clock()
    body = request("read")
    original = entry._bound

    def late(*args):
        value = original(*args)
        clock.t = tick
        return value

    sink = io.BytesIO()
    with pytest.MonkeyPatch.context() as patch:
        patch.setattr(entry, "_bound", late)
        code, wrapped = run(
            "file_io", body, io_api=Mem({body["path"]: b"hi"}), now_ticks=clock, stdout=sink
        )
    assert code == 2
    assert clock.t == tick
    assert sink.getvalue().count(b"\n") == 1
    assert wrapped.getvalue() == sink.getvalue()


def test_file_read_past_total_does_not_close():
    clock = Clock()
    body = request("read")

    class LateRead(Mem):
        def __init__(self):
            super().__init__({body["path"]: b"hi"})
            self.closes = []

        def read(self, handle, size):
            value = super().read(handle, size)
            clock.t = 100
            return value

        def close(self, handle):
            self.closes.append((handle, clock.t))
            super().close(handle)

    api = LateRead()
    with pytest.raises(FileHold) as caught:
        run("file_io", body, io_api=api, now_ticks=clock)
    hold = caught.value
    assert api.closes == []
    assert hold.api is api
    assert hold.resources.live()
    assert hold.detail["unattempted"]


def test_attributes_past_total_do_not_start_create_file():
    clock = Clock()

    class NativeLate(Native):
        def attributes(self, path):
            value = super().attributes(path)
            if path == NOTE:
                clock.t = 100
            return value

        def open_existing(self, *args):
            raise AssertionError("create_file")

    api = ProbeFileApi(FILE_ROOT, NativeLate({NOTE: b"hi"}))
    body = request("read", path=NOTE)
    code, _sink = run("file_io", body, io_api=api, now_ticks=clock)
    assert code == 2
    assert clock.t == 100
    assert not any(name == "open_existing" for name, *_rest in api.native.calls)


def test_fixed_root_unknown_close_keeps_the_factory_object(monkeypatch):
    kernel = Native({NOTE: b"hi"})
    kernel.fail = "close"
    made = []

    def factory():
        made.append(kernel)
        return kernel

    monkeypatch.setattr("scripts.ninfer_source_host.probe_io_winapi.RealProbeFileApi", factory)
    monkeypatch.setattr(clock_mod, "qpc_ticks", lambda: 1)
    monkeypatch.setattr(clock_mod, "qpc_frequency", lambda: 1)
    sink = io.BytesIO()

    class Stdout:
        buffer = sink

    monkeypatch.setattr(sys, "stdout", Stdout())
    with pytest.raises(FileHold) as caught:
        run_fixture("file_io", request("read", path=NOTE))
    hold = caught.value
    assert hold.native is kernel
    assert hold.api.native is kernel
    assert hold.resources is kernel.retained
    handle = hold.resources.handles[0].value
    assert handle in kernel.handles
    assert kernel.closed == [handle]
    with pytest.raises(IoError):
        hold.api.close(handle)
    assert kernel.closed == [handle]
    identity = hold.native
    api_identity = hold.api
    ref = weakref.ref(identity)
    api_ref = weakref.ref(api_identity)
    del made[:]
    gc.collect()
    assert ref() is identity
    assert api_ref() is hold.api
    assert hold.resources.handles[0].value == handle


@pytest.mark.parametrize("kind", _CANCELS)
def test_descendant_cancel_after_create_keeps_the_original_handles(monkeypatch, kind):
    _errors(monkeypatch)

    class Interrupt(Kernel):
        def WaitForSingleObject(self, handle, timeout):
            raise kind("injected_cancel")

    kernel = Interrupt()
    raw = _api(kernel)
    with pytest.raises(kind) as caught:
        try_descendant(NativeJobBoundary(raw))
    error = caught.value
    assert type(error) is kind
    assert error.native is raw
    assert error.resources is raw.retained
    assert error.resources.process == HIGH
    assert kernel.closed == []


def test_job_query_does_not_start_after_total(monkeypatch):
    clock = Clock()
    _errors(monkeypatch)

    class LateJob(Kernel):
        def IsProcessInJob(self, *args):
            result = super().IsProcessInJob(*args)
            clock.t = 100
            return result

    kernel = LateJob()
    gate = Deadline(bind_request_clock(request("hash"), clock))
    outcome = try_descendant(NativeJobBoundary(_api(kernel)), gate)
    assert outcome["classification"] == "deadline"
    assert outcome["detail"]["ticks"] == 100
    assert not any(call[0] == "query" for call in kernel.calls)
    assert not any(call[0] == "create" for call in kernel.calls)


def test_process_query_past_total_does_not_start_job_check(monkeypatch):
    clock = Clock()
    _errors(monkeypatch)

    class LateProcess(Kernel):
        def GetCurrentProcess(self):
            clock.t = 100
            return 11

    kernel = LateProcess()
    gate = Deadline(bind_request_clock(request("hash"), clock))
    with pytest.raises(DeadlineStop):
        try_descendant(NativeJobBoundary(_api(kernel)), gate)
    assert not any(call[0] == "job" for call in kernel.calls)
    assert not any(call[0] == "query" for call in kernel.calls)


@pytest.mark.parametrize("kind", _CANCELS)
def test_pipe_cancel_after_create_keeps_the_original_pipe(kind):
    class PipeCancel(Pipe):
        def read(self, handle, size):
            raise kind("injected_cancel")

    pipe = PipeCancel()
    with pytest.raises(kind) as caught:
        run("blocked_read", request("read"), native_api=pipe)
    error = caught.value
    assert error.native is pipe
    assert error.resources.native is pipe
    assert error.resources.handles == {"read": 101, "write": 102}
    assert pipe.closed == []
