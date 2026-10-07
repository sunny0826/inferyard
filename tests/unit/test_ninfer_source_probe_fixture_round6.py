"""库直接采样的 ClockError 也要停用；创建记录只转移一次。"""

import io

import pytest

from scripts.ninfer_source_host.probe_fixture_clock import ClockError
from scripts.ninfer_source_host.probe_fixture_descendant import DescendantHold, try_descendant
from scripts.ninfer_source_host.probe_io import ProbeFileApi
from scripts.ninfer_source_host.probe_io_limits import FILE_ROOT
from scripts.ninfer_source_host.probe_io_native import NativeJobBoundary
from tests.unit.test_ninfer_source_probe_fixture import run
from tests.unit.test_ninfer_source_probe_fixture_round2 import Kernel, _api, _errors
from tests.unit.test_ninfer_source_probe_io import HIGH, NOTE, Native, request


class _Direct(Native):
    def __init__(self, files, clock):
        super().__init__(files)
        self.clock = clock

    def read(self, *args):
        value = super().read(*args)
        self.clock.left = 3
        return value

    def write(self, *args):
        value = super().write(*args)
        self.clock.left = 3
        return value


class _Guard(Native):
    def __init__(self, files, clock):
        super().__init__(files)
        self.clock = clock

    def read(self, handle, size):
        value = super().read(handle, size)
        if size == 1:
            self.clock.left = 1
        return value

    def write(self, *args):
        value = super().write(*args)
        self.clock.left = 1
        return value


def _clock(kind):
    class Clock:
        frequency = 1
        left = 0
        fired = False

        def __call__(self):
            if self.left:
                self.left -= 1
                if self.left == 0:
                    self.fired = True
                    return True if kind == "bool" else 0
            return 1

    return Clock()


def _run_fault(operation, kind, native_type):
    clock = _clock(kind)
    files = {NOTE: b"hi"} if operation == "read" else {}
    native = native_type(files, clock)
    api = ProbeFileApi(FILE_ROOT, native)
    sink = io.BytesIO()
    with pytest.raises(ClockError) as caught:
        run(
            "file_io",
            request(operation, path=NOTE),
            io_api=api,
            now_ticks=clock,
            stdout=sink,
        )
    return caught.value, native, api, sink, clock


@pytest.mark.parametrize("operation", ("read", "write_new"))
@pytest.mark.parametrize("kind", ("bool", "regression"))
@pytest.mark.parametrize("native_type", (_Direct, _Guard))
def test_bool_and_regression_stop_after_recovery(operation, kind, native_type):
    error, native, api, sink, clock = _run_fault(operation, kind, native_type)
    assert clock.fired is True
    assert sink.getvalue() == b""
    assert native.closed == []
    assert not any(call[0] == "close" for call in native.calls)
    assert error.native is native
    assert error.resources is native.retained
    assert error.api is api
    handle = error.resources.handles[0].value
    assert handle in native.handles
    assert all(not item.closed and not item.unknown for item in error.resources.handles)
    if native_type is _Direct:
        assert error.io_result["bytes"] == 2
        assert error.io_result["data_b64"] is None
        if operation == "read":
            assert error.io_result["sha256"] is None
    assert clock() == 1
    assert native.closed == []
    assert sink.getvalue() == b""


def test_reused_boundary_create_failure_does_not_replay_closed_handles(monkeypatch):
    _errors(monkeypatch)

    class RefusedAfterOne(Kernel):
        def __init__(self):
            super().__init__()
            self.attempt = 0

        def CreateProcessW(self, *args):
            self.attempt += 1
            if self.attempt == 1:
                return super().CreateProcessW(*args)
            self.calls.append(("second_create_refused",))
            return 0

    kernel = RefusedAfterOne()
    raw = _api(kernel)
    boundary = NativeJobBoundary(raw)
    boundary._error = lambda ctypes: OSError(5, "actual_CreateProcessW_failure")
    first = try_descendant(boundary)
    assert first["classification"] == "job_terminated"
    assert first["handles"]["closed"] == {"thread": True, "process": True}
    assert kernel.closed == [HIGH + 1, HIGH]
    assert boundary.created_handles is None
    second = try_descendant(boundary)
    assert second["classification"] == "creation_rejected"
    assert second["handles"] == {
        "attempted": {},
        "closed": {},
        "unknown": [],
        "errors": [],
    }
    assert second["detail"]["observed"] == "create_process_refused"
    assert kernel.closed == [HIGH + 1, HIGH]
    assert boundary.created_handles is None
    assert getattr(raw, "retained", None) is None


def test_sample_failure_consumes_the_pair_before_a_later_rejection(monkeypatch):
    _errors(monkeypatch)
    state = {"fail": False}

    class Created(Kernel):
        def __init__(self):
            super().__init__()
            self.attempt = 0

        def CreateProcessW(self, *args):
            self.attempt += 1
            if self.attempt > 1:
                return 0
            result = super().CreateProcessW(*args)
            state["fail"] = True
            return result

    class Clock:
        frequency = 1
        task = 50
        total = 60

        def __call__(self):
            if state["fail"]:
                state["fail"] = False
                raise OSError("post_api_sample")
            return 1

    kernel = Created()
    raw = _api(kernel)
    boundary = NativeJobBoundary(raw)
    boundary._error = lambda ctypes: OSError(5, "actual_CreateProcessW_failure")
    from scripts.ninfer_source_host.probe_fixture_deadline import Deadline

    with pytest.raises(OSError, match="post_api_sample") as caught:
        try_descendant(boundary, Deadline(Clock()))
    error = caught.value
    assert error.resources.process == HIGH
    assert error.resources.thread == HIGH + 1
    assert error.native is raw
    assert error.resources is raw.retained
    assert kernel.closed == []
    assert boundary.created_handles is None
    second = try_descendant(boundary, Deadline(Clock()))
    assert second["classification"] == "creation_rejected"
    assert second["handles"]["unknown"] == []
    assert second["handles"]["closed"] == {}
    assert kernel.closed == []
    assert error.resources is raw.retained
    assert error.resources.process == HIGH


def test_unknown_close_is_not_retried_or_replayed(monkeypatch):
    _errors(monkeypatch)

    class Once(Kernel):
        def __init__(self):
            super().__init__()
            self.attempt = 0

        def CreateProcessW(self, *args):
            self.attempt += 1
            if self.attempt > 1:
                return 0
            return super().CreateProcessW(*args)

        def CloseHandle(self, handle):
            self.closed.append(handle)
            raise OSError("close_once")

    kernel = Once()
    raw = _api(kernel)
    boundary = NativeJobBoundary(raw)
    boundary._error = lambda ctypes: OSError(5, "actual_CreateProcessW_failure")
    with pytest.raises(DescendantHold) as caught:
        try_descendant(boundary)
    error = caught.value
    assert error.native is raw
    assert error.resources is raw.retained
    assert error.resources.process == HIGH
    assert error.resources.thread == HIGH + 1
    assert error.resources.report()["unknown"] == ["thread", "process"]
    assert kernel.closed == [HIGH + 1, HIGH]
    error.resources.close_once(boundary)
    assert kernel.closed == [HIGH + 1, HIGH]
    assert boundary.created_handles is None
    second = try_descendant(boundary)
    assert second["classification"] == "creation_rejected"
    assert second["handles"]["closed"] == {}
    assert kernel.closed == [HIGH + 1, HIGH]
    assert error.resources.report()["unknown"] == ["thread", "process"]
    assert error.resources is raw.retained
