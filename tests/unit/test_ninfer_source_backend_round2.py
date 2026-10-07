"""Stage45 二轮剩余缺陷。注入 WinAPI 与真实 Custodian，不调用真实系统 API。"""

import ctypes
from ctypes import wintypes
from unittest.mock import patch

import pytest

from scripts.ninfer_source_host.custody import (
    PROTOCOL,
    STAGE,
    BackendError,
    BackendStartError,
    Custodian,
    HostError,
)
from scripts.ninfer_source_host.windows_backend import ProcessRef, WindowsBackend
from scripts.ninfer_source_host.windows_backend_limits import PUMP_MAX, RETAIN_MAX
from scripts.ninfer_source_host.windows_backend_winapi import RealWindowsApi
from tests.unit.test_ninfer_source_backend import spec

HOST = "12345678-1234-1234-1234-123456789abc"
JOB = "22345678-1234-1234-1234-123456789abc"


class _Kernel:
    def __init__(self, stage, *, fail_handles=(), fail_count=None):
        self.stage = stage
        self.fail_handles = set(fail_handles)
        self.fail_count = fail_count
        self.pairs = 0
        self.closes = []

    def CreatePipe(self, read, write, _security, _size):
        self.pairs += 1
        if self.stage == "pipe" and self.pairs == 2:
            return 0
        read._obj.value = 99 + 2 * self.pairs
        write._obj.value = 100 + 2 * self.pairs
        return 1

    def SetHandleInformation(self, _handle, _flags, _mask):
        return 0 if self.stage == "inherit" else 1

    def CreateProcessW(self, *_args):
        if self.stage == "process":
            return 0
        info = _args[-1]._obj
        info.hProcess = 201
        info.hThread = 202
        return 1

    def CloseHandle(self, handle):
        self.closes.append(handle)
        if handle in self.fail_handles:
            if self.fail_count is None or self.closes.count(handle) <= self.fail_count:
                return 0
        return 1


def _raw(kernel):
    raw = RealWindowsApi.__new__(RealWindowsApi)
    raw._win32 = lambda: None
    raw._dll = lambda: (kernel, ctypes, wintypes)
    return raw


def _identity():
    return {
        "protocol": PROTOCOL,
        "host_id": HOST,
        "execution_sha256": "ab" * 32,
        "scripts_sha256": "cd" * 32,
        "stage": STAGE,
        "work_end_ticks": 100,
        "total_end_ticks": 200,
    }


def _job():
    return _identity() | {"job_id": JOB, "kind": "file_io", "task_end_ticks": 50}


def _owner(backend, ticks):
    return Custodian(_identity(), backend, ticks, 1, 1)


def _accept_windows_cwd():
    return patch(
        "scripts.ninfer_source_host.custody.os.path.isabs",
        lambda path: path == spec()["cwd"],
    )


@pytest.mark.parametrize("stage", ["pipe", "inherit", "process"])
def test_unknown_pipe_close_before_process_stays_with_custodian(stage):
    kernel = _Kernel(stage, fail_handles={101})
    host = WindowsBackend(api=_raw(kernel))
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    failure = caught.value
    ref = failure.ref
    assert ref is not None
    assert ref.resources is not None
    assert ref.resources.handles == tuple(handle for handle in ref.resources.handles if handle)
    assert 101 in ref.unknown
    assert kernel.closes.count(101) == 1
    assert set(ref.extra).isdisjoint(ref.unknown)
    assert set(ref.unknown).issubset(set(kernel.closes))
    assert not hasattr(kernel, "WaitForSingleObject")

    calls = []

    class Replay:
        def start(self, _spec):
            raise failure

        def exit_code(self, target):
            calls.append(("exit", target.process))
            return None

        def terminate(self, target):
            calls.append(("terminate", target.process))

        def kill(self, target):
            calls.append(("kill", target.process))

        def pump(self, target, _limit):
            calls.append(("pump", target.process))
            return {"stdout": b"", "stderr": b"", "eof": False}

        def close_pipes(self, target):
            calls.append(("pipes", target.process))

        def close_handle(self, target):
            calls.append(("handle", target.process, target.unknown, target.extra))
            raise BackendError()

    owner = _owner(Replay(), lambda: 0)
    with _accept_windows_cwd(), pytest.raises(HostError) as host_error:
        owner.start(_job(), spec())
    assert host_error.value.code == "host_backend"
    assert owner.owned_objects() == (ref,)
    decision = owner.decision()
    assert decision["custody_required"] is True
    assert decision["can_exit"] is False
    assert decision["success"] is False
    snapshot = owner.snapshot()["jobs"][0]
    assert snapshot["primary_error"] == "host_backend"
    assert calls == []
    owner.step()
    assert [name for name, *_rest in calls] == ["exit", "pump"]
    assert calls[0][1] is None
    assert calls[1][1] is None


def test_multiple_close_failures_keep_original_cause_and_unattempted():
    kernel = _Kernel("inherit", fail_handles={101, 103})
    host = WindowsBackend(api=_raw(kernel))
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    cause = caught.value.cause
    ref = caught.value.ref
    assert isinstance(cause, OSError)
    assert type(cause.__cause__) is OSError
    assert cause.__cause__.args == (0,)
    assert ref.unknown == (101,)
    assert 103 in ref.extra
    assert kernel.closes == [101]
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert kernel.closes == [101, 102, 103]
    assert ref.unknown == (101, 103)
    assert 104 in ref.extra
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert kernel.closes == [101, 102, 103]
    assert ref.unknown == (101, 103)


def test_confirmed_pipe_close_still_has_null_ref():
    kernel = _Kernel("pipe")
    host = WindowsBackend(api=_raw(kernel))
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    assert caught.value.ref is None
    assert kernel.closes == [101, 102]
    assert 0 not in kernel.closes
    assert None not in kernel.closes


def test_processless_ref_does_not_call_process_apis():
    calls = []

    class Api:
        def wait_zero(self, process):
            calls.append(("wait", process))

        def terminate(self, process):
            calls.append(("terminate", process))

        def kill(self, process):
            calls.append(("kill", process))

        def close(self, handle):
            calls.append(("close", handle))

    host = WindowsBackend(api=Api())
    ref = ProcessRef(None, None, None, None, extra=(104,), unknown=(101,))
    for method in (host.exit_code, host.terminate, host.kill):
        with pytest.raises(BackendError):
            method(ref)
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert calls == [("close", 104)]
    with pytest.raises(BackendError):
        host.close_handle(ref)
    assert calls == [("close", 104)]


class _Overflow:
    def __init__(self, failures):
        self.stdout = bytearray(b"ABCDEFGH" * (RETAIN_MAX // 8 + PUMP_MAX))
        self.prefix = bytes(self.stdout[:RETAIN_MAX])
        self.failures = failures
        self.reads = []
        self.terminated = []
        self.process = object()

    def spawn_suspended(self, *_args):
        return type(
            "Spawned",
            (),
            {
                "process": self.process,
                "thread": object(),
                "stdout": "stdout",
                "stderr": "stderr",
            },
        )()

    def create_job(self, *_args):
        return object()

    def assign_job(self, *_args):
        return None

    def resume(self, *_args):
        return None

    def wait_zero(self, process):
        assert process is self.process
        return 0x102

    def peek(self, pipe):
        if pipe == "stderr" and self.failures:
            self.failures -= 1
            raise OSError("stderr")
        if pipe == "stdout":
            return len(self.stdout), not self.stdout
        return 0, True

    def read(self, pipe, size):
        assert pipe == "stdout"
        assert 0 < size <= PUMP_MAX
        self.reads.append(size)
        chunk = bytes(self.stdout[:size])
        del self.stdout[:size]
        return chunk

    def terminate(self, process):
        self.terminated.append(process)

    def kill(self, _process):
        raise AssertionError("kill")

    def close(self, _handle):
        raise AssertionError("close")


@pytest.mark.parametrize("failures", [1, 9, 12])
def test_custodian_keeps_prefix_and_stops_on_hidden_overflow(failures):
    api = _Overflow(failures)
    clock = {"now": 0}
    owner = _owner(WindowsBackend(api=api), lambda: clock["now"])
    with _accept_windows_cwd():
        owner.start(_job(), spec())
    ref = owner.owned_objects()[0]
    for _index in range(40):
        owner.step()
        clock["now"] += 1
        if owner.snapshot()["jobs"][0]["state"] == "stopping":
            break
    snapshot = owner.snapshot()["jobs"][0]
    assert snapshot["stdout"] == api.prefix[:RETAIN_MAX]
    assert len(snapshot["stdout"]) == RETAIN_MAX
    assert snapshot["primary_error"] == "host_backend"
    assert "host_output_limit" in snapshot["cleanup_errors"]
    assert api.terminated == [api.process]
    assert owner.owned_objects() == (ref,)
    assert all(size <= PUMP_MAX for size in api.reads)
    assert max(api.reads) == PUMP_MAX
    assert sum(api.reads) >= RETAIN_MAX
    assert snapshot["stdout"] == api.prefix[:RETAIN_MAX]
    assert snapshot["stdout"] == api.prefix[: len(snapshot["stdout"])]
    reads_at_stop = len(api.reads)
    owner.step()
    assert len(api.reads) == reads_at_stop
    assert snapshot["state"] in {"stopping", "unknown"}


class _Distinct:
    """内容不同、长度不同的两流分块。每次读取都不超过调用者给出的额度。"""

    def __init__(self, stdout, stderr, fail=None):
        self.stdout = list(stdout)
        self.stderr = list(stderr)
        self.fail = fail
        self.reads = []

    def peek(self, pipe):
        if pipe == "stderr" and self.fail == "peek":
            self.fail = None
            raise OSError("stderr_peek")
        chunks = self.stdout if pipe == "stdout" else self.stderr
        return (len(chunks[0]), False) if chunks else (0, True)

    def read(self, pipe, size):
        if pipe == "stderr" and self.fail == "read":
            self.fail = None
            raise OSError("stderr_read")
        chunks = self.stdout if pipe == "stdout" else self.stderr
        chunk = chunks[0][:size]
        chunks[0] = chunks[0][size:]
        if not chunks[0]:
            chunks.pop(0)
        assert 0 < len(chunk) <= PUMP_MAX
        self.reads.append((pipe, len(chunk)))
        return chunk


def test_distinct_chunks_are_delivered_once_across_4096_and_32768():
    stdout = [b"ab", b"cde", b"Q" * 4090, b"TAIL"]
    stderr = [b"xy", b"uvw", b"Z" * 5000]
    api = _Distinct(stdout, stderr)
    host = WindowsBackend(api=api)
    ref = ProcessRef(object(), object(), "stdout", "stderr")
    delivered = {"stdout": b"", "stderr": b""}
    for _index in range(12):
        pumped = host.pump(ref, PUMP_MAX)
        for name in delivered:
            delivered[name] += pumped[name]
            assert len(pumped[name]) <= PUMP_MAX
        if pumped["eof"]:
            break
    assert delivered["stdout"] == b"ab" + b"cde" + b"Q" * 4090 + b"TAIL"
    assert delivered["stderr"] == b"xy" + b"uvw" + b"Z" * 5000
    assert ref.held_stdout == b"" and ref.held_stderr == b""
    assert sum(size for _pipe, size in api.reads) == len(delivered["stdout"]) + len(
        delivered["stderr"]
    )


@pytest.mark.parametrize("fail", ["peek", "read"])
def test_second_stream_failure_keeps_distinct_prefix(fail):
    api = _Distinct([b"ab", b"cde"], [b"xy"], fail=fail)
    host = WindowsBackend(api=api)
    ref = ProcessRef(
        object(),
        object(),
        "stdout",
        "stderr",
    )
    ref.held_stdout = b"PRE"
    with pytest.raises(BackendError):
        host.pump(ref, PUMP_MAX)
    assert ref.held_stdout.startswith(b"PRE")
    recovered = b""
    for _index in range(4):
        pumped = host.pump(ref, PUMP_MAX)
        recovered += pumped["stdout"]
        if pumped["eof"]:
            break
    assert recovered == b"PREabcde"
    assert ref.held_stdout == b""


class _RaisingKernel(_Kernel):
    def CloseHandle(self, handle):
        self.closes.append(handle)
        if handle == 101:
            raise OSError("injected_close_unknown")
        return 1


@pytest.mark.parametrize("stage", ["pipe", "inherit", "process"])
def test_close_exception_keeps_original_handle_and_cause(stage):
    kernel = _RaisingKernel(stage)
    host = WindowsBackend(api=_raw(kernel))
    with pytest.raises(BackendStartError) as caught:
        host.start(spec())
    failure = caught.value
    ref = failure.ref
    assert ref is not None
    assert ref.unknown == (101,)
    assert kernel.closes == [101]
    assert isinstance(failure.cause.__cause__, OSError)
    assert failure.cause.__cause__.__cause__.args == ("injected_close_unknown",)
    owner = _owner(
        type("Replay", (), {"start": staticmethod(lambda _spec: (_ for _ in ()).throw(failure))})(),
        lambda: 0,
    )
    with _accept_windows_cwd(), pytest.raises(HostError):
        owner.start(_job(), spec())
    decision = owner.decision()
    assert owner.owned_objects() == (ref,)
    assert decision["custody_required"] is True
    assert decision["can_exit"] is False
