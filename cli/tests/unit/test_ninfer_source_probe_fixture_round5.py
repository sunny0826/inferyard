"""资源先入账，再采样。QPC 的 OSError 粘滞后不再关闭或发布。"""

import gc
import io
import sys
import weakref

import pytest

import scripts.ninfer_source_host.probe_fixture_clock as clock_mod
from scripts.ninfer_source_host.probe_fixture import run_fixture
from scripts.ninfer_source_host.probe_fixture_clock import ClockError
from scripts.ninfer_source_host.probe_fixture_deadline import Deadline
from scripts.ninfer_source_host.probe_fixture_descendant import try_descendant
from scripts.ninfer_source_host.probe_io import ProbeFileApi
from scripts.ninfer_source_host.probe_io_limits import FILE_ROOT
from scripts.ninfer_source_host.probe_io_native import NativeJobBoundary
from tests.unit.test_ninfer_source_file_io import Mem
from tests.unit.test_ninfer_source_probe_fixture import Clock, run
from tests.unit.test_ninfer_source_probe_fixture_round2 import Kernel, _api, _errors
from tests.unit.test_ninfer_source_probe_io import HIGH, NOTE, Native, request

_KINDS = (OSError, KeyboardInterrupt, SystemExit)


class _Once(Clock):
    def __init__(self, kind):
        super().__init__()
        self.kind = kind
        self.armed = False
        self.fired = False

    def __call__(self):
        if self.armed and not self.fired:
            self.fired = True
            raise self.kind("post_resource_clock")
        return self.t


def _arming_native(kind, operation):
    clock = _Once(kind)

    class NativeOpen(Native):
        def open_existing(self, *args):
            value = super().open_existing(*args)
            clock.armed = True
            return value

        def create_new(self, *args):
            value = super().create_new(*args)
            clock.armed = True
            return value

    files = {NOTE: b"hi"} if operation == "read" else {}
    return clock, NativeOpen(files)


def _assert_kept(error, native, api, sink):
    assert error.native is native
    assert error.api is api
    assert error.resources is native.retained
    assert error.resources.handles
    handle = error.resources.handles[0].value
    assert handle is next(iter(native.handles))
    assert ("int", handle) in api._open
    assert native.closed == []
    assert not any(name == "close" for name, *_rest in native.calls)
    assert sink.getvalue() == b""
    assert all(not item.closed and not item.unknown for item in error.resources.handles)
    return handle


@pytest.mark.parametrize("operation", ("read", "write_new"))
@pytest.mark.parametrize("kind", _KINDS)
def test_returned_file_handle_survives_the_following_sample(operation, kind):
    clock, native = _arming_native(kind, operation)
    api = ProbeFileApi(FILE_ROOT, native)
    sink = io.BytesIO()
    with pytest.raises(kind) as caught:
        run("file_io", request(operation, path=NOTE), io_api=api, now_ticks=clock, stdout=sink)
    error = caught.value
    _assert_kept(error, native, api, sink)
    assert clock() == 1
    assert native.closed == []


@pytest.mark.parametrize("operation", ("read", "write_new"))
@pytest.mark.parametrize("kind", (OSError, KeyboardInterrupt))
def test_default_factory_sample_keeps_the_original_object(operation, kind, monkeypatch):
    clock_state = {"armed": False, "fired": False}

    def ticks():
        if clock_state["armed"] and not clock_state["fired"]:
            clock_state["fired"] = True
            raise kind("post_resource_clock")
        return 1

    kernel = Native({NOTE: b"hi"} if operation == "read" else {})

    def open_existing(self, *args):
        return _arm(Native.open_existing, self, clock_state, args)

    def create_new(self, *args):
        return _arm(Native.create_new, self, clock_state, args)

    kernel.open_existing = open_existing.__get__(kernel)
    kernel.create_new = create_new.__get__(kernel)
    made = []

    def factory():
        made.append(kernel)
        return kernel

    monkeypatch.setattr("scripts.ninfer_source_host.probe_io_winapi.RealProbeFileApi", factory)
    monkeypatch.setattr(clock_mod, "qpc_ticks", ticks)
    monkeypatch.setattr(clock_mod, "qpc_frequency", lambda: 1)
    sink = io.BytesIO()

    class Stdout:
        buffer = sink

    monkeypatch.setattr(sys, "stdout", Stdout())
    with pytest.raises(kind) as caught:
        run_fixture("file_io", request(operation, path=NOTE))
    error = caught.value
    handle = _assert_kept(error, kernel, error.api, sink)
    assert error.api.native is kernel
    native_ref = weakref.ref(kernel)
    api_ref = weakref.ref(error.api)
    del made[:]
    gc.collect()
    assert native_ref() is kernel
    assert api_ref() is error.api
    assert error.resources.handles[0].value == handle
    assert ticks() == 1
    assert kernel.closed == []


def _arm(method, self, state, args):
    value = method(self, *args)
    state["armed"] = True
    return value


def test_read_qpc_error_does_not_close_or_publish_after_recovery():
    clock = _Once(OSError)
    body = request("read", path=NOTE)

    class LateRead(Native):
        def read(self, handle, size):
            value = super().read(handle, size)
            if size == 1:
                clock.armed = True
            return value

    native = LateRead({NOTE: b"hi"})
    api = ProbeFileApi(FILE_ROOT, native)
    sink = io.BytesIO()
    with pytest.raises(OSError, match="post_resource_clock") as caught:
        run("file_io", body, io_api=api, now_ticks=clock, stdout=sink)
    error = caught.value
    assert error.io_result["operation"] == "read"
    assert error.io_result["bytes"] == 2
    assert any(call[0] == "read" for call in native.calls)
    _assert_kept(error, native, api, sink)
    assert clock() == 1
    assert native.closed == []


def test_close_qpc_error_does_not_start_close_handle():
    clock = Clock()
    clock.left = 0
    body = request("read")

    class LateSync(Mem):
        def __init__(self):
            super().__init__({body["path"]: b"hi"})
            self.closes = []

        def fsync(self, handle):
            clock.left = 3
            return super().fsync(handle)

        def close(self, handle):
            self.closes.append(handle)
            return super().close(handle)

    def sample():
        left = getattr(clock, "left", 0)
        if left:
            clock.left = left - 1
            if clock.left == 0:
                raise OSError("probe_qpc")
        return clock.t

    sample.frequency = 1
    api = LateSync()
    sink = io.BytesIO()
    with pytest.raises(OSError, match="probe_qpc") as caught:
        run("file_io", body, io_api=api, now_ticks=sample, stdout=sink)
    assert api.closes == []
    assert "close" not in api.calls
    assert sink.getvalue() == b""
    opened = caught.value.resources.handles[0]
    assert opened.closed is False
    assert opened.unknown is False
    assert sample() == 1
    assert api.closes == []


def test_successful_close_stays_closed_when_later_qpc_fails():
    clock = Clock()
    clock.left = 0
    body = request("read")

    class LateClose(Mem):
        def __init__(self):
            super().__init__({body["path"]: b"hi"})
            self.closes = []

        def close(self, handle):
            self.closes.append(handle)
            super().close(handle)
            clock.left = 2

    def sample():
        left = getattr(clock, "left", 0)
        if left:
            clock.left = left - 1
            if clock.left == 0:
                raise OSError("probe_qpc")
        return clock.t

    sample.frequency = 1
    api = LateClose()
    sink = io.BytesIO()
    with pytest.raises(OSError, match="probe_qpc") as caught:
        run("file_io", body, io_api=api, now_ticks=sample, stdout=sink)
    assert api.closes == [1]
    opened = caught.value.resources.handles[0]
    assert opened.closed is True
    assert opened.unknown is False
    assert sink.getvalue() == b""
    assert sample() == 1
    assert api.closes == [1]


def test_publish_qpc_error_does_not_write_when_clock_recovers():
    clock = _Once(OSError)
    clock.armed = True
    sink = io.BytesIO()
    with pytest.raises(OSError, match="post_resource_clock"):
        run("output", request("read"), now_ticks=clock, stdout=sink)
    assert sink.getvalue() == b""
    assert clock() == 1
    assert sink.getvalue() == b""


@pytest.mark.parametrize("kind", _KINDS)
def test_create_process_sample_keeps_original_handles(kind, monkeypatch):
    _errors(monkeypatch)
    state = {"created": False, "raised": False}

    class GateClock:
        frequency = 1
        task = 50
        total = 60

        def __call__(self):
            if state["created"] and not state["raised"]:
                state["raised"] = True
                raise kind("clock_after_create")
            return 1

    class Created(Kernel):
        def CreateProcessW(self, *args):
            result = super().CreateProcessW(*args)
            state["created"] = True
            return result

    kernel = Created()
    raw = _api(kernel)
    boundary = NativeJobBoundary(raw)
    gate_clock = GateClock()
    with pytest.raises(kind) as caught:
        try_descendant(boundary, Deadline(gate_clock))
    error = caught.value
    assert error.native is raw
    assert error.resources is raw.retained
    assert boundary.created_handles is None
    assert error.resources.process == HIGH
    assert error.resources.thread == HIGH + 1
    assert kernel.closed == []
    assert any(call[0] == "create" for call in kernel.calls)
    assert gate_clock() == 1
    assert kernel.closed == []


@pytest.mark.parametrize("kind", _KINDS)
def test_pipe_sample_keeps_original_ends(kind):
    state = {"created": False, "raised": False}

    class PipeClock(Clock):
        def __call__(self):
            if state["created"] and not state["raised"]:
                state["raised"] = True
                raise kind("clock_after_pipe")
            return self.t

    class Pipe:
        injected = True

        def __init__(self):
            self.closed = []

        def create_pipe(self):
            state["created"] = True
            return 101, 102

        def close(self, handle):
            self.closed.append(handle)

    pipe = Pipe()
    sink = io.BytesIO()
    with pytest.raises(kind) as caught:
        run("blocked_read", request("read"), native_api=pipe, now_ticks=PipeClock(), stdout=sink)
    error = caught.value
    assert error.native is pipe
    assert error.resources is pipe.retained
    assert error.resources.handles == {"read": 101, "write": 102}
    assert pipe.closed == []
    assert sink.getvalue() == b""


def test_clock_regression_after_open_still_holds_the_file():
    clock = Clock()

    class Reverse(Native):
        def open_existing(self, *args):
            value = super().open_existing(*args)
            clock.t = 0
            return value

    native = Reverse({NOTE: b"hi"})
    api = ProbeFileApi(FILE_ROOT, native)
    sink = io.BytesIO()
    with pytest.raises(ClockError, match="fixture_clock_regression") as caught:
        run("file_io", request("read", path=NOTE), io_api=api, now_ticks=clock, stdout=sink)
    error = caught.value
    assert error.resources.handles[0].value in native.handles
    assert error.native is native
    assert native.closed == []
    assert sink.getvalue() == b""
