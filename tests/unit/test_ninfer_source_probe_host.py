"""ProbeHost 注入回归：真实 Custodian、fake Backend 与内存时钟。

不启动 worker、子进程、socket 或 Windows API，也不能当作原生证据。
"""

import builtins
import os
import socket
import subprocess
import threading
import time
from collections import deque
from contextlib import contextmanager

import pytest

from scripts.ninfer_source_host.custody import STAGE, BackendError, Custodian, HostError
from scripts.ninfer_source_host.probe_host import EXPERIMENT_STAGE, ProbeHost, main

HOST_ID = "11111111-2222-4333-8444-abcdefabcdef"
JOB_ID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
EXEC_SHA = "a" * 64
SCRIPTS_SHA = "b" * 64
EXE_SHA = "c" * 64
SCRIPT_SHA = "d" * 64
OUTPUT_LIMIT = 32768


def identity(**over):
    base = {
        "protocol": "ninfer-source-host/1",
        "host_id": HOST_ID,
        "execution_sha256": EXEC_SHA,
        "scripts_sha256": SCRIPTS_SHA,
        "stage": STAGE,
        "work_end_ticks": 1740,
        "total_end_ticks": 1800,
    }
    base.update(over)
    return base


def job(**over):
    base = {**identity(), "job_id": JOB_ID, "kind": "file_io", "task_end_ticks": 1500}
    base.update(over)
    return base


def spec():
    return {
        "argv": ("python", "worker.py"),
        "cwd": "/tmp/work",
        "executable_sha256": EXE_SHA,
        "script_sha256": SCRIPT_SHA,
        "no_descendants": True,
    }


class FakeBackend:
    """冻结 Backend 的内存假对象。calls 只记录方法与原 ref。"""

    def __init__(self):
        self.calls = []
        self.started = []
        self.pump_script = {}
        self.exit_script = {}
        self.errors = {}

    def start(self, spec_value):
        ref = object()
        self.started.append(ref)
        self.calls.append(("start", ref))
        return ref

    def _scripted(self, script, ref, default):
        queue = script.get(ref)
        if not queue:
            return default
        item = queue.popleft()
        if isinstance(item, Exception):
            raise item
        return item

    def pump(self, ref, limit):
        assert limit == 4096
        self.calls.append(("pump", ref))
        return self._scripted(self.pump_script, ref, {"stdout": b"", "stderr": b"", "eof": True})

    def exit_code(self, ref):
        self.calls.append(("exit_code", ref))
        return self._scripted(self.exit_script, ref, 0)

    def _op(self, method, ref):
        self.calls.append((method, ref))
        error = self.errors.get(method)
        if error is not None:
            raise error

    def terminate(self, ref):
        self._op("terminate", ref)

    def kill(self, ref):
        self._op("kill", ref)

    def close_pipes(self, ref):
        self._op("close_pipes", ref)

    def close_handle(self, ref):
        self._op("close_handle", ref)


def make(clock_start=1000):
    clock = [clock_start]
    backend = FakeBackend()
    host = ProbeHost(identity(), backend, lambda: clock[0], 1000, 60)
    return host, backend, clock


def methods(backend):
    return [name for name, _ref in backend.calls]


def chunk(stdout=b"", stderr=b"", eof=False):
    return {"stdout": stdout, "stderr": stderr, "eof": eof}


def hold_open(backend, ref):
    backend.exit_script[ref] = deque([None, None, None])
    backend.pump_script[ref] = deque([chunk(b"a", b"e"), chunk(b"b", b"f"), chunk()])


def start_open(host, backend):
    host.start(job(), spec())
    ref = host.owned_objects()[0]
    hold_open(backend, ref)
    return ref


@contextmanager
def forbid_control_io():
    """控制路径期间，文件、sleep、线程、子进程和 socket 桩直接失败。"""

    def deny(*_args, **_kwargs):
        raise AssertionError("control path used a forbidden API")

    saved = {
        "open": builtins.open,
        "os_open": os.open,
        "sleep": time.sleep,
        "thread": threading.Thread,
        "socket": socket.socket,
        "popen": subprocess.Popen,
        "run": subprocess.run,
    }
    builtins.open = deny
    os.open = deny
    time.sleep = deny
    threading.Thread = deny
    socket.socket = deny
    subprocess.Popen = deny
    subprocess.run = deny
    try:
        yield
    finally:
        builtins.open = saved["open"]
        os.open = saved["os_open"]
        time.sleep = saved["sleep"]
        threading.Thread = saved["thread"]
        socket.socket = saved["socket"]
        subprocess.Popen = saved["popen"]
        subprocess.run = saved["run"]


def separated_resume(host):
    return host.resume(HOST_ID, EXEC_SHA)


def separated_bad_resume(host):
    return host.resume(HOST_ID, "f" * 64)


def test_two_calls_resume_same_custodian_and_ref():
    host, backend, _clock = make()
    owner = host._custodian
    assert type(owner) is Custodian
    with forbid_control_io():
        ref = start_open(host, backend)
        first = separated_resume(host)
        second = separated_resume(host)
    assert host._custodian is owner
    assert host.owned_objects() == (ref,)
    assert first["jobs"][0]["stdout"] == b"a"
    assert second["jobs"][0]["stdout"] == b"ab"
    assert {item for _name, item in backend.calls if item is not None} == {ref}


def test_resume_does_not_construct_another_custodian(monkeypatch):
    import scripts.ninfer_source_host.probe_host as mod

    made = []
    real = mod.Custodian

    class Spy(real):
        def __init__(self, *args, **kwargs):
            made.append(self)
            super().__init__(*args, **kwargs)

    monkeypatch.setattr(mod, "Custodian", Spy)
    backend = FakeBackend()
    host = mod.ProbeHost(identity(), backend, lambda: 1000, 1000, 60)
    with forbid_control_io():
        ref = start_open(host, backend)
        separated_resume(host)
        separated_resume(host)
    assert made == [host._custodian]
    assert host.owned_objects() == (ref,)


def test_wrong_identity_resume_makes_zero_backend_calls():
    host, backend, _clock = make()
    with forbid_control_io():
        start_open(host, backend)
        before = list(backend.calls)
        with pytest.raises(HostError) as exc:
            separated_bad_resume(host)
    assert exc.value.code == "host_identity"
    assert backend.calls == before


def test_unknown_close_cannot_exit_and_keeps_ref():
    host, backend, _clock = make()
    with forbid_control_io():
        host.start(job(), spec())
        ref = backend.started[0]
        backend.errors["close_pipes"] = BackendError()
        entry = host.step()["jobs"][0]
        decision = host.decision()
        before = list(backend.calls)
        again = host.step()
        evidence = host.evidence()
    assert entry["state"] == "unknown"
    assert entry["primary_error"] == "host_cleanup"
    assert entry["close_attempts"] == 2
    assert entry["close_successes"] == 1
    assert decision["can_exit"] is False
    assert decision["success"] is False
    assert decision["custody_required"] is True
    assert again["jobs"][0]["state"] == "unknown"
    assert backend.calls == before
    assert host.owned_objects() == (ref,)
    assert evidence["decision"]["can_exit"] is False
    assert evidence["retained_count"] == 1
    assert evidence["snapshot"]["stage"] == STAGE


@pytest.mark.parametrize(
    "ident, patch, code",
    [
        (identity(total_end_ticks=1740), None, "host_identity"),
        (identity(work_end_ticks=1800), None, "host_identity"),
        (identity(stage=46), None, "host_identity"),
        (identity(), {"task_end_ticks": 1000}, "host_job"),
        (identity(), {"task_end_ticks": 1741}, "host_job"),
    ],
)
def test_task_work_total_rejected_with_zero_backend_calls(ident, patch, code):
    backend = FakeBackend()
    if patch is None:
        with forbid_control_io(), pytest.raises(HostError) as exc:
            ProbeHost(ident, backend, lambda: 1000, 1000, 60)
    else:
        host = ProbeHost(ident, backend, lambda: 1000, 1000, 60)
        with forbid_control_io(), pytest.raises(HostError) as exc:
            host.start(job(**patch), spec())
    assert exc.value.code == code
    assert backend.calls == []


def test_deadline_error_stays_after_later_close():
    host, backend, clock = make()
    with forbid_control_io():
        host.start(job(), spec())
        ref = backend.started[0]
        backend.exit_script[ref] = deque([None, 0])
        backend.pump_script[ref] = deque([chunk(), chunk(b"kept", eof=True)])
        clock[0] = 1500
        stopping = host.step()["jobs"][0]
        clock[0] = 1560
        closed = host.step()["jobs"][0]
        decision = host.decision()
    assert stopping["state"] == "stopping"
    assert stopping["primary_error"] == "host_deadline"
    assert closed["state"] == "closed"
    assert closed["primary_error"] == "host_deadline"
    assert closed["stdout"] == b"kept"
    assert decision["can_exit"] is True
    assert decision["success"] is False
    assert decision["reason"] == "host_deadline"


def test_output_limit_keeps_exact_prefix_and_sticky_error():
    host, backend, _clock = make()
    with forbid_control_io():
        host.start(job(), spec())
        ref = backend.started[0]
        backend.exit_script[ref] = deque([None, 0])
        backend.pump_script[ref] = deque(
            [chunk(b"S" * 40000, b"E" * 40000), chunk(b"MORE", b"MORE", eof=True)]
        )
        limited = host.step()["jobs"][0]
        closed = host.step()["jobs"][0]
        decision = host.decision()
    assert limited["state"] == "stopping"
    assert limited["stdout"] == b"S" * OUTPUT_LIMIT
    assert limited["stderr"] == b"E" * OUTPUT_LIMIT
    assert type(limited["stdout"]) is bytes
    assert type(limited["stderr"]) is bytes
    assert limited["primary_error"] == "host_output_limit"
    assert methods(backend).count("terminate") == 1
    assert closed["state"] == "closed"
    assert closed["stdout"] == b"S" * OUTPUT_LIMIT
    assert closed["stderr"] == b"E" * OUTPUT_LIMIT
    assert closed["primary_error"] == "host_output_limit"
    assert decision["success"] is False
    assert decision["reason"] == "host_output_limit"


def test_cancel_then_drain_counts_close_once():
    host, backend, _clock = make()
    with forbid_control_io():
        ref = start_open(host, backend)
        host.step()
        cancelled = host.cancel(JOB_ID, "operator_cancel")
        backend.exit_script[ref] = deque([0])
        backend.pump_script[ref] = deque([chunk(eof=True)])
        closed = host.step()["jobs"][0]
    assert cancelled["jobs"][0]["state"] == "stopping"
    assert cancelled["jobs"][0]["primary_error"] == "host_cancelled"
    assert closed["state"] == "closed"
    assert closed["close_attempts"] == 2
    assert closed["close_successes"] == 2
    assert methods(backend).count("terminate") == 1
    assert methods(backend).count("kill") == 0
    assert methods(backend).count("close_pipes") == 1
    assert methods(backend).count("close_handle") == 1
    assert host.owned_objects() == ()


def test_snapshot_and_evidence_are_independent_copies():
    host, backend, _clock = make()
    with forbid_control_io():
        ref = start_open(host, backend)
        separated_resume(host)
        snap = host.snapshot()
        evidence = host.evidence()
        again = host.evidence()
    assert evidence["experiment_stage"] == 46
    assert EXPERIMENT_STAGE == 46
    assert set(evidence) == {
        "experiment_stage",
        "identity",
        "snapshot",
        "decision",
        "retained_count",
    }
    assert evidence["identity"]["stage"] == STAGE
    assert evidence["snapshot"]["stage"] == STAGE
    assert evidence["decision"]["stage"] == STAGE
    assert evidence["retained_count"] == 1
    assert evidence["snapshot"]["jobs"][0]["stdout"] == b"a"
    assert type(evidence["snapshot"]["jobs"][0]["stdout"]) is bytes
    assert type(evidence["snapshot"]["jobs"][0]["stderr"]) is bytes
    snap["jobs"][0]["stdout"] = b"tampered"
    snap["jobs"][0]["cleanup_errors"].append("tampered")
    snap["stage"] = 46
    evidence["identity"]["stage"] = 46
    evidence["snapshot"]["stage"] = 46
    evidence["snapshot"]["jobs"][0]["state"] = "tampered"
    evidence["decision"]["jobs"].append({})
    fresh = host.snapshot()
    fresh_evidence = host.evidence()
    assert fresh["stage"] == STAGE
    assert fresh["jobs"][0]["stdout"] == b"a"
    assert fresh["jobs"][0]["state"] == "running"
    assert fresh_evidence["experiment_stage"] == 46
    assert fresh_evidence["identity"]["stage"] == STAGE
    assert fresh_evidence["snapshot"]["stage"] == STAGE
    assert fresh_evidence["snapshot"]["jobs"][0]["state"] == "running"
    assert evidence["snapshot"] is not snap
    assert evidence["snapshot"]["jobs"] is not again["snapshot"]["jobs"]
    assert evidence["decision"]["jobs"] is not evidence["snapshot"]["jobs"]
    assert evidence["identity"] is not again["identity"]
    assert host.owned_objects() == (ref,)


def test_caller_identity_mutation_does_not_rewrite_library_stage():
    source = identity()
    host = ProbeHost(source, FakeBackend(), lambda: 1000, 1000, 60)
    source["stage"] = 46
    source["host_id"] = JOB_ID
    evidence = host.evidence()
    assert evidence["identity"]["stage"] == STAGE
    assert evidence["identity"]["host_id"] == HOST_ID
    assert host.snapshot()["stage"] == STAGE


@pytest.mark.parametrize(
    "argv",
    [
        None,
        [],
        [""],
        ["probe_host.py"],
        ["probe_host.py", "--allow"],
        ["probe_host.py", "D:\\lab46-source-host"],
        ["probe_host.py", "1", "2"],
    ],
)
def test_main_always_rejects(argv, monkeypatch):
    import scripts.ninfer_source_host.probe_host as mod

    monkeypatch.setenv("NINFER_ALLOW_SOURCE_HOST", "1")
    monkeypatch.setenv("LAB46_SOURCE_ROOT", "D:\\lab46-source-host")

    def boom(*_args, **_kwargs):
        raise AssertionError("main constructed a host")

    monkeypatch.setattr(mod, "ProbeHost", boom)
    monkeypatch.setattr(mod, "Custodian", boom)
    with forbid_control_io():
        assert main(argv) == 2
        assert mod.main(argv) == 2
