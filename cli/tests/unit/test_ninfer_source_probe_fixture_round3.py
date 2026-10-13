"""Stage46 第三轮注入。只写 sink、调用边界、默认工厂保管。不回读 stdout，不启动真实进程。"""

import gc
import io
import sys
import weakref

import pytest

import scripts.ninfer_source_host.probe_fixture as fixture
import scripts.ninfer_source_host.probe_fixture_clock as clock_mod
from scripts.ninfer_source_host.probe_fixture import run_fixture
from scripts.ninfer_source_host.probe_fixture_custody import PipeHold
from scripts.ninfer_source_host.probe_fixture_descendant import DescendantHold
from scripts.ninfer_source_host.probe_fixture_frames import loads_frame
from scripts.ninfer_source_host.probe_io_limits import OUTPUT_PREFIX, STREAM_LIMIT
from tests.unit.test_ninfer_source_file_io import Mem
from tests.unit.test_ninfer_source_probe_fixture import Clock, run
from tests.unit.test_ninfer_source_probe_fixture_round2 import HIGH, Kernel, _api, _errors
from tests.unit.test_ninfer_source_probe_io import request


class WriteOnly:
    """只有 write 和 flush。没有 getvalue，也没有 buffer。"""

    def __init__(self):
        self.parts = []
        self.flushes = 0

    def write(self, data):
        self.parts.append(bytes(data))
        return len(data)

    def flush(self):
        self.flushes += 1


def _public_stdout(monkeypatch, sink):
    monkeypatch.setattr(clock_mod, "qpc_ticks", lambda: 1)
    monkeypatch.setattr(clock_mod, "qpc_frequency", lambda: 1)

    class Stdout:
        buffer = sink

    monkeypatch.setattr(sys, "stdout", Stdout())


def test_public_write_only_file_io_succeeds_from_write_count(monkeypatch):
    sink = WriteOnly()
    _public_stdout(monkeypatch, sink)
    body = request("read")
    code = run_fixture("file_io", body, io_api=Mem({body["path"]: b"hi"}))
    raw = b"".join(sink.parts)
    assert code == 0
    assert sink.flushes == 1
    assert raw.startswith(b"{")
    assert sum(len(part) for part in sink.parts) == len(raw)


def test_public_write_only_output_succeeds_from_write_count(monkeypatch):
    sink = WriteOnly()
    _public_stdout(monkeypatch, sink)
    code = run_fixture("output", request("hash"))
    raw = b"".join(sink.parts)
    assert code == 0
    assert sink.flushes == 1
    assert raw.startswith(OUTPUT_PREFIX)
    assert len(raw) > STREAM_LIMIT
    assert not raw.startswith(b"{")


def test_entry_parse_past_total_is_not_success():
    clock = Clock()
    original = fixture.loads_frame

    def late(raw):
        value = original(raw)
        clock.t = 100
        return value

    body = request("read")
    with pytest.MonkeyPatch.context() as patch:
        patch.setattr(fixture, "loads_frame", late)
        code, sink = run("file_io", body, io_api=Mem({body["path"]: b"hi"}), now_ticks=clock)
    assert code == 2
    assert clock.t == 100
    lines = [line for line in sink.getvalue().splitlines() if line.startswith(b"{")]
    assert len(lines) == 1
    assert loads_frame(lines[0])["status"] == "ok"


def test_result_write_past_total_does_not_flush():
    clock = Clock()
    body = request("read")

    class LateWrite(io.BytesIO):
        def write(self, data):
            self.calls = getattr(self, "calls", [])
            self.calls.append(("write", clock.t))
            clock.t = 100
            return super().write(data)

        def flush(self):
            raise AssertionError("flush")

    stdout = LateWrite()
    code, sink = run(
        "file_io", body, io_api=Mem({body["path"]: b"hi"}), now_ticks=clock, stdout=stdout
    )
    assert code == 2
    assert stdout.calls == [("write", 1)]
    assert sink.getvalue() != b""
    assert clock.t == 100


def test_ready_write_past_total_does_not_flush_or_publish():
    clock = Clock()

    class Pipe:
        def __init__(self):
            self.order = []

        def create_pipe(self):
            self.order.append("create_pipe")
            return 101, 102

        def publish_ready(self, _frame):
            self.order.append("ready")

        def read(self, handle, size):
            self.order.append("read")
            return b""

        def close(self, handle):
            self.order.append(("close", handle))

        def write_sink(self):
            return None

    class LateWrite:
        def __init__(self):
            self.calls = []

        def write(self, data):
            self.calls.append(("write", clock.t, len(data)))
            clock.t = 100
            return len(data)

        def flush(self):
            self.calls.append(("flush", clock.t))
            raise AssertionError("flush")

    pipe = Pipe()
    sink = LateWrite()
    with pytest.raises(PipeHold) as caught:
        run("blocked_read", request("read"), native_api=pipe, now_ticks=clock, stdout=sink)
    assert caught.value.native is pipe
    assert pipe.order == ["create_pipe"]
    assert [item[0] for item in sink.calls] == ["write"]
    assert sink.calls[0][1] == 1
    assert clock.t == 100


def test_create_pipe_is_sampled_before_ready():
    clock = Clock()

    class Pipe:
        def __init__(self):
            self.order = []

        def create_pipe(self):
            self.order.append(("create_pipe", clock.t))
            clock.t = 100
            return 101, 102

        def publish_ready(self, _frame):
            self.order.append("ready")

        def close(self, handle):
            self.order.append(("close", handle))

    pipe = Pipe()
    with pytest.raises(PipeHold) as caught:
        run("blocked_read", request("read"), native_api=pipe, now_ticks=clock)
    assert caught.value.resources.handles == {"read": 101, "write": 102}
    assert pipe.order == [("create_pipe", 1)]
    assert caught.value.native is pipe


def test_first_pipe_close_past_total_does_not_close_the_other_end():
    clock = Clock()

    class Pipe:
        def __init__(self):
            self.closed = []

        def create_pipe(self):
            return 101, 102

        def publish_ready(self, _frame):
            return None

        def read(self, handle, size):
            return b""

        def close(self, handle):
            self.closed.append((handle, clock.t))
            if handle == 102:
                clock.t = 100

    pipe = Pipe()
    with pytest.raises(PipeHold) as caught:
        run("blocked_read", request("read"), native_api=pipe, now_ticks=clock)
    assert pipe.closed == [(102, 1)]
    assert caught.value.resources.tried == {"write": True, "read": False}
    assert caught.value.native is pipe


def test_wait_past_total_does_not_read_exit_code(monkeypatch):
    clock = Clock()
    kernel = Kernel(exit_code=1)
    _errors(monkeypatch)

    def wait(handle, timeout):
        kernel.calls.append(("wait", handle, timeout, clock.t))
        clock.t = 61
        return 0

    kernel.WaitForSingleObject = wait
    raw = _api(kernel)
    with pytest.raises(DescendantHold) as caught:
        run("descendant", request("hash"), native_api=raw, now_ticks=clock)
    assert kernel.calls[-1][0] == "wait"
    assert not any(call[0] == "exit" for call in kernel.calls)
    assert kernel.closed == []
    assert caught.value.native is raw
    assert caught.value.resources.process == HIGH


def test_thread_close_past_total_does_not_close_process(monkeypatch):
    clock = Clock()
    kernel = Kernel(exit_code=1)
    _errors(monkeypatch)

    def close(handle):
        kernel.closed.append((handle, clock.t))
        clock.t = 61
        return 1

    kernel.CloseHandle = close
    raw = _api(kernel)
    with pytest.raises(DescendantHold) as caught:
        run("descendant", request("hash"), native_api=raw, now_ticks=clock)
    assert kernel.closed == [(HIGH + 1, 1)]
    assert caught.value.resources.tried.get("process") is not True
    assert caught.value.native is raw
    assert caught.value.resources.process == HIGH


def _factory(monkeypatch, kernel):
    made = []

    def factory():
        native = _api(kernel)
        made.append(native)
        return native

    monkeypatch.setattr(
        "scripts.ninfer_source_host.probe_io_winapi.RealProbeFileApi",
        factory,
    )
    monkeypatch.setattr(clock_mod, "qpc_ticks", lambda: 1)
    monkeypatch.setattr(clock_mod, "qpc_frequency", lambda: 1)
    sink = WriteOnly()

    class Stdout:
        buffer = sink

    monkeypatch.setattr(sys, "stdout", Stdout())
    return made


def test_default_factory_hold_survives_collection(monkeypatch):
    kernel = Kernel(wait=258, exit_code=259)
    _errors(monkeypatch)
    made = _factory(monkeypatch, kernel)
    with pytest.raises(DescendantHold) as caught:
        run_fixture("descendant", request("hash"))
    hold = caught.value
    assert len(made) == 1
    assert hold.native is made[0]
    assert hold.resources is made[0].retained
    assert hold.resources.process == HIGH
    assert kernel.closed == []
    identity = hold.native
    ref = weakref.ref(identity)
    resource_ref = weakref.ref(hold.resources)
    del made[:]
    gc.collect()
    assert ref() is identity
    assert resource_ref() is hold.resources
    assert hold.native is identity


def test_default_factory_wait_error_keeps_the_same_oserror(monkeypatch):
    kernel = Kernel()
    _errors(monkeypatch)

    def wait(handle, timeout):
        kernel.calls.append(("wait", handle, timeout))
        raise OSError("injected_wait_error")

    kernel.WaitForSingleObject = wait
    made = _factory(monkeypatch, kernel)
    with pytest.raises(OSError, match="injected_wait_error") as caught:
        run_fixture("descendant", request("hash"))
    error = caught.value
    assert error.native is made[0]
    assert error.resources is made[0].retained
    assert error.resources.process == HIGH
    assert error.resources.thread == HIGH + 1
    assert kernel.closed == []
    identity = error.native
    ref = weakref.ref(identity)
    del made[:]
    gc.collect()
    assert ref() is error.native
    assert error.native is identity


def test_default_factory_unknown_close_is_not_retried(monkeypatch):
    kernel = Kernel(exit_code=1, close_ok=False)
    _errors(monkeypatch, 6)
    made = _factory(monkeypatch, kernel)
    with pytest.raises(DescendantHold) as caught:
        run_fixture("descendant", request("hash"))
    hold = caught.value
    assert hold.native is made[0]
    assert hold.resources is made[0].retained
    assert isinstance(hold.__cause__, OSError)
    assert hold.__cause__.native is made[0]
    assert kernel.closed.count(HIGH) == 1
    assert kernel.closed.count(HIGH + 1) == 1
    before = list(kernel.closed)
    hold.resources.close_once(hold.native)
    assert kernel.closed == before
    identity = hold.native
    ref = weakref.ref(identity)
    del made[:]
    gc.collect()
    assert ref() is identity


def test_default_factory_pipe_hold_survives_collection(monkeypatch):
    made = []
    ticks = {"now": 1}

    class Pipe:
        injected = True

        def __init__(self):
            self.closed = []

        def create_pipe(self):
            return 101, 102

        def read(self, handle, size):
            ticks["now"] = 100
            return b"x"

        def close(self, handle):
            self.closed.append(handle)

    def factory():
        native = Pipe()
        made.append(native)
        return native

    monkeypatch.setattr(
        "scripts.ninfer_source_host.probe_io_winapi.RealProbeFileApi",
        factory,
    )
    monkeypatch.setattr(clock_mod, "qpc_ticks", lambda: ticks["now"])
    monkeypatch.setattr(clock_mod, "qpc_frequency", lambda: 1)
    sink = WriteOnly()

    class Stdout:
        buffer = sink

    monkeypatch.setattr(sys, "stdout", Stdout())
    with pytest.raises(PipeHold) as caught:
        run_fixture("blocked_read", request("read"))
    hold = caught.value
    assert hold.native is made[0]
    assert hold.resources.native is made[0]
    assert made[0].closed == []
    assert hold.detail["bytes"] == 1
    identity = hold.native
    ref = weakref.ref(identity)
    pipe_ref = weakref.ref(hold.resources)
    del made[:]
    gc.collect()
    assert ref() is identity
    assert pipe_ref() is hold.resources
