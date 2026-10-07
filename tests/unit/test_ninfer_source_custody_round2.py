"""Custodian 第二轮 review 行为回归:每次 Backend 返回后统一复核原时钟/期限。

协议 mock:仅 fake Backend 与可变时钟,不构成 Windows 原生证据。对应
artifacts/ninfer-adaptation-wave1-20261004/stage45-kimi-review/reproduce-round2.py 四场景。
"""

import pytest

from scripts.ninfer_source_host.custody import (
    PROTOCOL,
    STAGE,
    BackendStartError,
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


def make(clock_start=100, grace=10):
    clock = [clock_start]
    backend = FakeBackend()
    custodian = Custodian(identity(), backend, lambda: clock[0], 1000, grace)
    return custodian, backend, clock


def methods(backend):
    return [method for method, _ in backend.calls]


# ---- A: 退出确认跨 task 期限 —— 期限记账与停止信号分离 ---------------------------------


def test_late_exit_confirmation_records_deadline_no_terminate():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    ref = backend.started[0]

    def late_exit(ref_value):
        assert ref_value is ref
        backend.calls.append(("exit_code", ref_value))
        clock[0] = 201
        return 0

    backend.exit_code = late_exit
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "closed"
    assert entry["returncode"] == 0
    assert entry["original_exited"] is True
    assert entry["primary_error"] == "host_deadline"
    assert methods(backend).count("terminate") == 0
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_deadline"


# ---- B: start/close_handle 返回边界的统一复核 ------------------------------------------


def test_start_return_clock_regression_recorded_sticky():
    custodian, backend, clock = make()
    original_start = backend.start

    def bad_clock_start(spec_value):
        ref = original_start(spec_value)
        clock[0] = 99
        return ref

    backend.start = bad_clock_start
    assert custodian.start(job(), spec()) == JOB_ID
    entry = custodian.snapshot()["jobs"][0]
    assert entry["state"] == "running"
    assert entry["primary_error"] == "host_clock"
    clock[0] = 101
    assert custodian.step()["jobs"][0]["state"] == "closed"
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_clock"


def test_start_error_with_ref_clock_regression_keeps_ref():
    custodian, backend, clock = make()
    created = object()

    def bad_clock_start(spec_value):
        backend.calls.append(("start", None))
        clock[0] = 99
        raise BackendStartError(created, cause=OSError("job assign failed"))

    backend.start = bad_clock_start
    with pytest.raises(HostError) as exc:
        custodian.start(job(), spec())
    assert exc.value.code == "host_backend"
    entry = custodian.snapshot()["jobs"][0]
    assert entry["state"] == "unknown"
    assert entry["primary_error"] == "host_backend"
    assert entry["cleanup_errors"] == ["host_clock"]
    assert custodian.owned_objects() == (created,)
    clock[0] = 101
    assert custodian.resume(HOST_ID, EXEC_SHA)["jobs"][0]["state"] == "closed"
    assert {ref for _, ref in backend.calls if ref is not None} == {created}
    result = custodian.decision()
    assert result["success"] is False
    assert result["reason"] == "host_backend"


def test_start_cross_total_keeps_job_without_terminate():
    custodian, backend, clock = make()
    original_start = backend.start

    def cross_total_start(spec_value):
        ref = original_start(spec_value)
        clock[0] = 1100
        return ref

    backend.start = cross_total_start
    assert custodian.start(job(), spec()) == JOB_ID
    snap = custodian.snapshot()
    entry = snap["jobs"][0]
    assert entry["state"] == "running"
    assert entry["primary_error"] == "host_deadline"
    assert snap["custody_required"] is True
    assert custodian.owned_objects() == (backend.started[0],)
    calls_before = list(backend.calls)
    custodian.step()
    assert backend.calls == calls_before
    assert methods(backend).count("terminate") == 0


def test_close_handle_return_clock_regression_recorded():
    custodian, backend, clock = make()
    custodian.start(job(), spec())

    def bad_clock_close(ref):
        backend.calls.append(("close_handle", ref))
        clock[0] = 99

    backend.close_handle = bad_clock_close
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "closed"
    assert entry["close_successes"] == 2
    assert entry["primary_error"] == "host_clock"
    assert custodian.owned_objects() == ()
    clock[0] = 101
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_clock"


def test_close_handle_cross_total_releases_known_closed_ref():
    custodian, backend, clock = make()
    custodian.start(job(), spec())

    def cross_total_close(ref):
        backend.calls.append(("close_handle", ref))
        clock[0] = 1100

    backend.close_handle = cross_total_close
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "closed"
    assert entry["close_successes"] == 2
    assert entry["primary_error"] is None
    assert custodian.owned_objects() == ()
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_deadline"


# ---- C: 输出超限跨 total —— 记账与信号分离,无嵌套/后续自动调用 ---------------------------


def test_overflow_at_total_records_without_terminate():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    ref = backend.started[0]

    def running_exit(ref_value):
        backend.calls.append(("exit_code", ref_value))
        return None

    pumps = []

    def overflow_at_total(ref_value, limit):
        assert limit == 4096
        backend.calls.append(("pump", ref_value))
        pumps.append(1)
        if len(pumps) == 9:
            clock[0] = 1100
            return {"stdout": b"x", "stderr": b"", "eof": False}
        return {"stdout": b"x" * 4096, "stderr": b"", "eof": False}

    backend.exit_code = running_exit
    backend.pump = overflow_at_total
    for _ in range(8):
        custodian.step()
    assert len(custodian.snapshot()["jobs"][0]["stdout"]) == 32768
    snap = custodian.step()
    entry = snap["jobs"][0]
    assert entry["stdout"] == b"x" * 32768
    assert entry["state"] == "running"
    assert entry["primary_error"] == "host_deadline"
    assert entry["cleanup_errors"] == ["host_output_limit"]
    assert methods(backend).count("terminate") == 0
    assert methods(backend).count("kill") == 0
    assert snap["custody_required"] is True
    assert custodian.owned_objects() == (ref,)
    calls_before = list(backend.calls)
    custodian.step()
    assert backend.calls == calls_before


# ---- cancel 信号返回边界:terminate 返回后同样统一复核 ------------------------------------


def test_cancel_terminate_cross_total_records_no_further_calls():
    custodian, backend, clock = make()
    custodian.start(job(), spec())

    def cross_total_terminate(ref):
        backend.calls.append(("terminate", ref))
        clock[0] = 1100

    backend.terminate = cross_total_terminate
    snap = custodian.cancel(JOB_ID, "operator_cancel")
    entry = snap["jobs"][0]
    assert entry["state"] == "stopping"
    assert entry["primary_error"] == "host_cancelled"
    assert entry["cleanup_errors"] == ["host_deadline"]
    assert methods(backend).count("terminate") == 1
    assert snap["custody_required"] is True
    calls_before = list(backend.calls)
    custodian.step()
    assert backend.calls == calls_before
    assert methods(backend).count("kill") == 0


def test_cancel_terminate_clock_regression_recorded():
    custodian, backend, clock = make()
    custodian.start(job(), spec())

    def bad_clock_terminate(ref):
        backend.calls.append(("terminate", ref))
        clock[0] = 99

    backend.terminate = bad_clock_terminate
    entry = custodian.cancel(JOB_ID, "operator_cancel")["jobs"][0]
    assert entry["state"] == "stopping"
    assert entry["primary_error"] == "host_cancelled"
    assert entry["cleanup_errors"] == ["host_clock"]
    clock[0] = 101
    assert custodian.step()["jobs"][0]["state"] == "closed"
    assert methods(backend).count("terminate") == 1
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_cancelled"
