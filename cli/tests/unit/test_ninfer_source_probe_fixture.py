"""Stage46 诊断夹具正向注入回归。不创建真实子进程、socket、管道或 WinDLL。"""

import ast
import builtins
import io
import socket
import subprocess
from pathlib import Path

import pytest

from scripts.ninfer_source_host.file_io_codec import IoError
from scripts.ninfer_source_host.probe_fixture import main, run_fixture
from scripts.ninfer_source_host.probe_fixture_clock import ClockError, bind_request_clock
from scripts.ninfer_source_host.probe_fixture_custody import PipeHold
from scripts.ninfer_source_host.probe_fixture_frames import loads_frame
from scripts.ninfer_source_host.probe_io_limits import OUTPUT_BODY, OUTPUT_PREFIX, STREAM_LIMIT
from tests.unit.test_ninfer_source_file_io import digest
from tests.unit.test_ninfer_source_probe_io import HOST, JOB, request

SOURCE = Path(__file__).resolve().parents[2] / "scripts" / "ninfer_source_host"


class Clock:
    frequency = 1

    def __init__(self, start=1):
        self.t = start

    def __call__(self):
        return self.t


def run(mode, body, **kwargs):
    sink = kwargs.pop("stdout", io.BytesIO())
    code, frame = run_fixture.__globals__["_run_fixture"](
        mode, body, stdout=sink, now_ticks=kwargs.pop("now_ticks", Clock()), **kwargs
    )
    return code, FrameSink(sink, frame)


class FrameSink:
    """只暴露调用方 sink 上实际写出的字节，不回退到 worker 的私有返回值。"""

    def __init__(self, sink, frame):
        self.sink = sink
        self.frame = frame

    def getvalue(self):
        getter = getattr(self.sink, "getvalue", None)
        if getter is not None:
            data = getter()
            if type(data) is bytes:
                return data
        buffer = getattr(self.sink, "buffer", None)
        if isinstance(buffer, bytearray):
            return bytes(buffer)
        return b""


class Pipe:
    def __init__(self, fail=None):
        self.fail = fail
        self.order = []
        self.closed = []
        self.injected = True

    def create_pipe(self):
        self.order.append("create_pipe")
        return (101, 102)

    def publish_ready(self, frame):
        self.order.append("ready")
        if self.fail == "publish":
            raise OSError("publish")

    def read(self, handle, size):
        self.order.append(("read", handle, size))
        if "ready" not in self.order:
            raise AssertionError("read before ready")
        if self.fail == "block":
            raise AssertionError("blocking read returned")
        if self.fail == "read":
            raise OSError("read")
        return b""

    def close(self, handle):
        self.order.append(("close", handle))
        self.closed.append(handle)
        if getattr(self, "close_error", None) == handle:
            raise OSError("close")


class Sink:
    def __init__(self, fail=None):
        self.fail = fail
        self.buffer = bytearray()

    def write(self, data):
        if self.fail == "write":
            raise OSError("ready_write_failed")
        self.buffer.extend(data)
        return len(data)

    def flush(self):
        if self.fail == "flush":
            raise OSError("flush")

    def getvalue(self):
        frame = getattr(self, "frame", None)
        if frame is not None and self.fail != "output":
            return frame
        return bytes(self.buffer)


class Child:
    def __init__(self, kind):
        self.kind = kind
        self.calls = []
        self.injected = True

    def try_descendant(self, argv):
        self.calls.append(argv)
        if self.kind == "rejected":
            return {"classification": "creation_rejected", "detail": {"observed": "assign_refused"}}
        if self.kind == "terminated":
            return {
                "classification": "job_terminated",
                "detail": {"observed": "job_limit_terminated"},
            }
        return {"classification": "silent", "detail": {}}


def test_main_returns_two_without_fixture_or_environment(monkeypatch):
    calls = []
    monkeypatch.setattr(
        "scripts.ninfer_source_host.probe_fixture.run_fixture",
        lambda *args, **kwargs: calls.append(1),
    )
    monkeypatch.setenv("LAB46_ROOT", "C:" + chr(92) + "escape")
    assert main([]) == 2
    assert main(["probe", "file_io", "{}"]) == 2
    entry = run_fixture.__globals__["_module_entry"]
    assert entry(["probe_fixture", "blocked_read", "{}"]) == 2
    assert calls == []


def test_fixture_sources_do_not_import_process_tools():
    banned = {"socket", "subprocess"}
    paths = (
        SOURCE / "probe_fixture.py",
        SOURCE / "probe_fixture_custody.py",
        SOURCE / "probe_fixture_deadline.py",
        SOURCE / "probe_fixture_descendant.py",
        SOURCE / "probe_fixture_files.py",
        SOURCE / "probe_fixture_frames.py",
        SOURCE / "probe_fixture_publish.py",
        SOURCE / "probe_fixture_worker.py",
    )
    for path in paths:
        tree = ast.parse(path.read_text())
        imported = []
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imported.extend(alias.name.split(".")[0] for alias in node.names)
            elif isinstance(node, ast.ImportFrom) and node.module:
                imported.append(node.module.split(".")[0])
        assert banned.isdisjoint(imported)
        source = path.read_text()
        assert "WinDLL" not in source
        assert "Popen" not in source
        assert "time.sleep" not in source


def test_default_clock_refuses_before_qpc_on_mac():
    with pytest.raises(OSError, match="probe_qpc_unavailable"):
        run_fixture("file_io", request("read"), io_api=object())


def test_injected_clock_rejects_expired_bool_and_regression():
    body = request("read")
    late = Clock(100)
    with pytest.raises(ClockError, match="fixture_deadline"):
        run("file_io", body, io_api=object(), now_ticks=late)
    bad = Clock()
    bad.frequency = True
    with pytest.raises(ClockError):
        bind_request_clock(body, bad)
    reverse = Clock(2)

    def backwards():
        reverse.t -= 1
        return reverse.t

    backwards.frequency = 1
    clock = bind_request_clock(body, backwards)
    assert clock() == 1
    with pytest.raises(ClockError, match="fixture_clock_regression"):
        clock()


def test_file_io_error_result_returns_nonzero_and_keeps_reason():
    payload = b"hi"
    body = request("read", expected_sha256="00" * 32)

    class Api:
        injected = True

        def is_reparse(self, path):
            return False

        def open_read(self, path):
            return 7

        def read(self, handle, size):
            return payload if size > 1 else b""

        def flush(self, handle):
            return None

        def fsync(self, handle):
            return None

        def close(self, handle):
            return None

    code, sink = run("file_io", body, io_api=Api())
    frame = loads_frame(sink.getvalue())
    assert code == 2
    assert frame["status"] == "error"
    assert frame["result"]["primary_error"] == "io_failure"
    assert frame["result"]["bytes"] == len(payload)
    assert frame["host_id"] == HOST
    assert frame["job_id"] == JOB


def test_file_io_success_uses_original_task_deadline():
    payload = b"hi"
    body = request("read")
    seen = {}

    class Api:
        def is_reparse(self, path):
            return False

        def open_read(self, path):
            seen["path"] = path
            return 7

        def read(self, handle, size):
            if seen.get("done"):
                return b""
            seen["done"] = True
            return payload

        def flush(self, handle):
            return None

        def fsync(self, handle):
            return None

        def close(self, handle):
            return None

    code, sink = run("file_io", body, io_api=Api())
    frame = loads_frame(sink.getvalue())
    assert code == 0
    assert frame["result"]["primary_error"] is None
    assert frame["result"]["sha256"] == digest(payload)
    assert seen["path"].endswith("note.txt")


@pytest.mark.parametrize("step", ["write", "flush", "publish", "read"])
def test_blocked_ready_failures_close_original_handles(step):
    native = Pipe("publish" if step == "publish" else "read" if step == "read" else None)
    sink = Sink(step if step in {"write", "flush"} else None)
    code, returned = run("blocked_read", request("read"), native_api=native, stdout=sink)
    raw = returned.getvalue()
    lines = [line for line in raw.splitlines() if line.startswith(b"{")]
    assert code == 2
    assert native.closed == [102, 101]
    if step == "write":
        assert lines == []
        return
    frame = loads_frame(lines[-1])
    assert frame["status"] == "error"
    assert frame["result"]["completed"] is False
    assert frame["result"]["primary_error"] == "fixture_failure"


def test_blocked_close_error_does_not_replace_read_error():
    native = Pipe("read")
    native.close_error = 102
    body = request("read")
    # 读失败和第一次关闭失败都留在同一个 PipeHold 上。未知关闭不重试，也不收成返回码。
    with pytest.raises(PipeHold) as caught:
        run("blocked_read", body, native_api=native)
    hold = caught.value
    assert hold.native is native
    assert hold.resources.native is native
    assert hold.detail["primary_type"] == "OSError"
    assert "OSError" in [item["error"] for item in hold.detail["cleanup"]]
    assert native.closed == [102, 101]
    assert native.order.count(("close", 102)) == 1
    before = list(native.closed)
    hold.resources.close_with_gate(None)
    assert native.closed == before


def test_blocked_completion_is_not_reported_as_proved_blocking():
    native = Pipe()
    code, sink = run("blocked_read", request("read"), native_api=native)
    lines = sink.sink.getvalue().splitlines()
    assert loads_frame(lines[0])["status"] == "ready"
    frame = loads_frame(lines[-1])
    assert code == 0
    assert frame["status"] == "returned"
    assert frame["result"]["blocking_proved"] is False
    assert native.order[:3] == ["create_pipe", "ready", ("read", 101, 1)]


def test_output_stdout_is_raw_prefix_not_json():
    code, wrapped = run("output", request("hash"))
    raw = wrapped.sink.getvalue()
    assert code == 0
    assert raw.startswith(OUTPUT_PREFIX)
    assert not raw.startswith(b"{")
    assert b"data_b64" not in raw
    assert OUTPUT_BODY in raw
    assert len(raw) > STREAM_LIMIT


@pytest.mark.parametrize(
    ("kind", "status"),
    [("rejected", "creation_rejected"), ("terminated", "job_terminated")],
)
def test_descendant_reports_distinct_classifications(kind, status):
    native = Child(kind)
    code, sink = run("descendant", request("hash"), native_api=native)
    frame = loads_frame(sink.getvalue())
    assert code == 0
    assert frame["status"] == status
    assert frame["result"]["classification"] == status
    assert native.calls[0][0] != "python"
    assert native.calls[0][1:] == ("-c", "raise SystemExit(0)")


def test_descendant_silent_skip_is_rejected():
    with pytest.raises(ValueError, match="fixture_descendant"):
        run("descendant", request("hash"), native_api=Child("silent"))


def test_unknown_mode_and_identity_do_not_start(monkeypatch):
    monkeypatch.setattr(
        subprocess,
        "Popen",
        lambda *_args, **_kwargs: (_ for _ in ()).throw(AssertionError("popen")),
    )
    with pytest.raises(ValueError, match="fixture_mode"):
        run_fixture.__globals__["_run_fixture"](
            "asset",
            request("read"),
            io_api=None,
            native_api=object(),
            now_ticks=Clock(),
            stdout=io.BytesIO(),
        )
    with pytest.raises(IoError):
        run("file_io", {"stage": True}, native_api=object())


def test_socket_subprocess_and_open_remain_unused(monkeypatch):
    def banned(*_args, **_kwargs):
        raise AssertionError("real call")

    monkeypatch.setattr(socket, "socket", banned)
    monkeypatch.setattr(subprocess, "Popen", banned)
    monkeypatch.setattr(builtins, "open", banned)
    code, _sink = run("descendant", request("hash"), native_api=Child("rejected"))
    assert code == 0
