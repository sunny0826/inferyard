"""Custodian 期限/时钟专项回归:task 期限、原总期限边界、跨期复核、时钟失败 sticky。

协议 mock:仅 fake Backend 与序列时钟,不构成 Windows 原生证据。
"""

from collections import deque

import pytest

from scripts.ninfer_source_host.custody import (
    PROTOCOL,
    STAGE,
    BackendError,
    Custodian,
    HostError,
)

HOST_ID = "11111111-2222-4333-8444-abcdefabcdef"
JOB_ID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
EXEC_SHA = "a" * 64
SCRIPTS_SHA = "b" * 64
EXE_SHA = "c" * 64
SCRIPT_SHA = "d" * 64


def identity(**over):
    base = {
        "protocol": PROTOCOL,
        "host_id": HOST_ID,
        "execution_sha256": EXEC_SHA,
        "scripts_sha256": SCRIPTS_SHA,
        "stage": STAGE,
        "work_end_ticks": 1000,
        "total_end_ticks": 1100,
    }
    base.update(over)
    return base


def job(**over):
    base = {**identity(), "job_id": JOB_ID, "kind": "file_io", "task_end_ticks": 200}
    base.update(over)
    return base


def spec(**over):
    base = {
        "argv": ("python", "worker.py"),
        "cwd": "/tmp/work",
        "executable_sha256": EXE_SHA,
        "script_sha256": SCRIPT_SHA,
        "no_descendants": True,
    }
    base.update(over)
    return base


class FakeBackend:
    """冻结 Backend 接口的内存 fake;calls 记录 (方法, ref) 用于计数与引用恒等断言。"""

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


class Clock:
    """确定性序列时钟:每次采样弹出下一个值,用尽后保持最后值。"""

    def __init__(self, values):
        self.values = deque(values)
        self.current = values[0]

    def __call__(self):
        if self.values:
            self.current = self.values.popleft()
        return self.current

    def set(self, value):
        self.values.clear()
        self.current = value


def make(clock_start=100, grace=10):
    clock = Clock([clock_start])
    backend = FakeBackend()
    custodian = Custodian(identity(), backend, clock, 1000, grace)
    return custodian, backend, clock


def methods(backend):
    return [method for method, _ in backend.calls]


def chunk(stdout=b"", stderr=b"", eof=False):
    return {"stdout": stdout, "stderr": stderr, "eof": eof}


# ---- task 期限:min(task, work) 限制任务工作 ------------------------------------------


def test_task_deadline_stops_before_work_and_denies_success():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([None, 0])
    clock.set(201)
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "stopping"
    assert entry["primary_error"] == "host_deadline"
    assert methods(backend).count("terminate") == 1
    assert custodian.step()["jobs"][0]["state"] == "closed"
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_deadline"


def test_task_deadline_boundary_exact_and_before():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([None, None, 0])
    backend.pump_script[ref] = deque([chunk(), chunk(), chunk(eof=True)])
    clock.set(199)
    assert custodian.step()["jobs"][0]["state"] == "running"
    assert methods(backend).count("terminate") == 0
    clock.set(200)
    assert custodian.step()["jobs"][0]["state"] == "stopping"
    assert methods(backend).count("terminate") == 1


def test_cross_task_deadline_during_pump_rechecked():
    clock = Clock([100, 100, 201, 201, 201, 201])
    backend = FakeBackend()
    custodian = Custodian(identity(), backend, clock, 1000, 10)
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([None, 0])
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "stopping"
    assert entry["primary_error"] == "host_deadline"
    assert methods(backend).count("terminate") == 1
    assert custodian.step()["jobs"][0]["state"] == "closed"
    assert custodian.decision()["success"] is False


# ---- 原总期限:到 total 保留对象,三入口零自动调用 ----------------------------------------


def test_work_deadline_grace_window_may_end_at_total():
    custodian, backend, clock = make(grace=60)
    custodian.start(job(task_end_ticks=1000), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([None, None, None])
    backend.pump_script[ref] = deque([chunk(), chunk(), chunk(eof=True)])
    clock.set(1000)
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "stopping"
    assert entry["primary_error"] == "host_deadline"
    assert methods(backend).count("terminate") == 1
    clock.set(1059)
    custodian.step()
    assert methods(backend).count("kill") == 0
    calls_before = list(backend.calls)
    clock.set(1100)
    snap = custodian.step()
    assert backend.calls == calls_before
    assert snap["custody_required"] is True
    assert custodian.owned_objects() == (ref,)
    result = custodian.decision()
    assert result["can_exit"] is False
    assert result["success"] is False
    assert result["reason"] == "host_deadline"


def test_step_at_total_makes_no_backend_calls():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    clock.set(1100)
    snap = custodian.step()
    assert backend.calls == [("start", ref)]
    assert snap["jobs"][0]["state"] == "running"
    assert snap["custody_required"] is True
    assert custodian.owned_objects()[0] is ref


def test_resume_at_total_makes_no_backend_calls():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    clock.set(1100)
    snap = custodian.resume(HOST_ID, EXEC_SHA)
    assert backend.calls == [("start", backend.started[0])]
    assert snap["custody_required"] is True


def test_cancel_at_total_makes_no_backend_calls():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    clock.set(1100)
    snap = custodian.cancel(JOB_ID, "operator_cancel")
    assert backend.calls == [("start", backend.started[0])]
    entry = snap["jobs"][0]
    assert entry["state"] == "running"
    assert entry["primary_error"] is None


def test_kill_after_grace_within_total_closes():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    custodian.cancel(JOB_ID, "operator_cancel")
    assert methods(backend).count("terminate") == 1
    clock.set(110)
    snap = custodian.step()
    assert methods(backend).count("kill") == 1
    assert snap["jobs"][0]["state"] == "closed"
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_cancelled"


def test_terminate_failure_keeps_ref_no_retry():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.errors["terminate"] = BackendError()
    backend.exit_script[ref] = deque([0])
    entry = custodian.cancel(JOB_ID, "io_error")["jobs"][0]
    assert entry["state"] == "unknown"
    assert entry["primary_error"] == "host_cancelled"
    assert entry["cleanup_errors"] == ["host_backend"]
    assert methods(backend).count("terminate") == 1
    assert custodian.owned_objects() == (ref,)
    clock.set(110)
    snap = custodian.step()
    assert methods(backend).count("terminate") == 1
    assert methods(backend).count("kill") == 1
    assert snap["jobs"][0]["state"] == "closed"
    assert custodian.owned_objects() == ()


def test_kill_failure_keeps_ref_no_retry():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.errors["kill"] = BackendError()
    backend.exit_script[ref] = deque([None, 0])
    custodian.cancel(JOB_ID, "deadline")
    clock.set(110)
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "unknown"
    assert entry["primary_error"] == "host_cancelled"
    assert entry["cleanup_errors"] == ["host_backend"]
    assert methods(backend).count("kill") == 1
    clock.set(120)
    custodian.step()
    assert methods(backend).count("kill") == 1
    assert custodian.snapshot()["jobs"][0]["state"] == "closed"


def test_cross_total_during_pump_keeps_facts_and_ref():
    # start 返回后复核占一个采样点;pump 返回后跨 total,期限错误记账、停止信号不再发出。
    clock = Clock([100, 100, 100, 100, 1100])
    backend = FakeBackend()
    custodian = Custodian(identity(), backend, clock, 1000, 10)
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([None])
    backend.pump_script[ref] = deque([chunk(b"data", eof=True)])
    snap = custodian.step()
    entry = snap["jobs"][0]
    assert entry["state"] == "running"
    assert entry["stdout"] == b"data"
    assert entry["primary_error"] == "host_deadline"
    assert "terminate" not in methods(backend)
    assert snap["custody_required"] is True
    assert custodian.owned_objects() == (ref,)
    calls_before = list(backend.calls)
    custodian.step()
    assert backend.calls == calls_before


def test_cross_total_during_close_skips_handle():
    clock = Clock([100, 100, 100, 100, 100, 1100])
    backend = FakeBackend()
    custodian = Custodian(identity(), backend, clock, 1000, 10)
    custodian.start(job(), spec())
    ref = backend.started[0]
    snap = custodian.step()
    entry = snap["jobs"][0]
    assert entry["state"] == "exited"
    assert entry["close_attempts"] == 1
    assert entry["close_successes"] == 1
    assert "close_handle" not in methods(backend)
    assert snap["custody_required"] is True
    assert custodian.owned_objects() == (ref,)


# ---- 时钟失败:sticky 记录,期限内可收尾但不可恢复为成功 ------------------------------------


def test_clock_failure_sticky_denies_success():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    clock.set(99)
    with pytest.raises(HostError) as exc:
        custodian.step()
    assert exc.value.code == "host_clock"
    assert custodian.snapshot()["jobs"][0]["primary_error"] == "host_clock"
    clock.set(101)
    backend.exit_script[ref] = deque([0])
    assert custodian.step()["jobs"][0]["state"] == "closed"
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_clock"


def test_clock_failure_during_step_stops_further_calls():
    clock = Clock([100, 100, 100, 99])
    backend = FakeBackend()
    custodian = Custodian(identity(), backend, clock, 1000, 10)
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([None, 0])
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "running"
    assert entry["primary_error"] == "host_clock"
    assert "pump" not in methods(backend)
    clock.set(101)
    assert custodian.step()["jobs"][0]["state"] == "closed"
    assert custodian.decision()["success"] is False
    assert custodian.decision()["reason"] == "host_clock"


# ---- 输入校验边界(零 Backend 调用) ------------------------------------------------------


@pytest.mark.parametrize(
    "patch",
    [
        {"stage": 45.0},
        {"stage": True},
        {"work_end_ticks": 1000.0},
        {"work_end_ticks": True},
        {"total_end_ticks": 1100.0},
        {"total_end_ticks": True},
    ],
)
def test_job_identity_float_and_bool_rejected(patch):
    custodian, backend, _ = make()
    with pytest.raises(HostError) as exc:
        custodian.start(job(**patch), spec())
    assert exc.value.code == "host_identity"
    assert backend.calls == []


@pytest.mark.parametrize(
    "patch",
    [
        {"host_id": HOST_ID + "\n"},
        {"execution_sha256": EXEC_SHA + "\n"},
        {"scripts_sha256": SCRIPTS_SHA + "\n"},
    ],
)
def test_identity_trailing_lf_rejected(patch):
    with pytest.raises(HostError) as exc:
        Custodian(identity(**patch), FakeBackend(), lambda: 100, 1000, 10)
    assert exc.value.code == "host_identity"


def test_job_id_trailing_lf_rejected():
    custodian, backend, _ = make()
    with pytest.raises(HostError) as exc:
        custodian.start(job(job_id=JOB_ID + "\n"), spec())
    assert exc.value.code == "host_job"
    assert backend.calls == []


@pytest.mark.parametrize("bad", ["\ud800", "\udfff", "ok\ud800x"])
def test_surrogate_argv_rejected(bad):
    custodian, backend, _ = make()
    with pytest.raises(HostError) as exc:
        custodian.start(job(), spec(argv=("python", bad)))
    assert exc.value.code == "host_job"
    assert backend.calls == []


def test_astral_argv_counts_utf16_units():
    custodian, backend, _ = make()
    assert custodian.start(job(), spec(argv=("python", "\U0001d4b3"))) == JOB_ID
    custodian2, backend2, _ = make()
    with pytest.raises(HostError) as exc:
        custodian2.start(job(), spec(argv=("\U0001d4b3" * 6001,)))
    assert exc.value.code == "host_job"
    assert backend2.calls == []
