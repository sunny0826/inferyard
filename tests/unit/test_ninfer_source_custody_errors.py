"""Custodian 错误路径回归:Backend 失败、期限、超限、关闭计数与同实例接手。

协议 mock:仅 fake Backend,不构成 Windows 原生 Job Object/管道/句柄证据。
"""

from collections import deque

import pytest

from scripts.ninfer_source_host.custody import (
    PROTOCOL,
    STAGE,
    BackendError,
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
        "work_end_ticks": 1740,
        "total_end_ticks": 1800,
    }
    base.update(over)
    return base


def job(**over):
    base = {**identity(), "job_id": JOB_ID, "kind": "file_io", "task_end_ticks": 1500}
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
        self.start_error = None
        self.pump_script = {}
        self.exit_script = {}
        self.errors = {}

    def start(self, spec_value):
        if self.start_error is not None:
            self.calls.append(("start", None))
            raise self.start_error
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
    custodian = Custodian(identity(), backend, lambda: clock[0], 1000, 60)
    return custodian, backend, clock


def methods(backend):
    return [method for method, _ in backend.calls]


def refs_used(backend):
    return {ref for _, ref in backend.calls if ref is not None}


def chunk(stdout=b"", stderr=b"", eof=False):
    return {"stdout": stdout, "stderr": stderr, "eof": eof}


# ---- start 失败 --------------------------------------------------------------------


def test_start_plain_backend_error_not_registered():
    custodian, backend, _ = make()
    backend.start_error = BackendError()
    with pytest.raises(HostError) as exc:
        custodian.start(job(), spec())
    assert exc.value.code == "host_backend"
    assert custodian.owned_objects() == ()
    assert custodian.snapshot()["jobs"] == []
    backend.start_error = None
    assert custodian.start(job(), spec()) == JOB_ID


def test_start_error_without_ref_not_registered():
    custodian, backend, _ = make()
    backend.start_error = BackendStartError(None, cause=OSError("spawn failed"))
    with pytest.raises(HostError) as exc:
        custodian.start(job(), spec())
    assert exc.value.code == "host_backend"
    assert custodian.owned_objects() == ()
    assert custodian.snapshot()["jobs"] == []
    backend.start_error = None
    assert custodian.start(job(), spec()) == JOB_ID


def test_start_partial_creation_keeps_original_ref():
    custodian, backend, _ = make()
    created = object()
    backend.start_error = BackendStartError(created, cause=OSError("job assign failed"))
    with pytest.raises(HostError) as exc:
        custodian.start(job(), spec())
    assert exc.value.code == "host_backend"
    with pytest.raises(HostError) as exc_again:
        custodian.start(job(), spec())
    assert exc_again.value.code == "host_job"
    assert custodian.owned_objects() == (created,)
    entry = custodian.snapshot()["jobs"][0]
    assert entry["state"] == "unknown"
    assert entry["primary_error"] == "host_backend"
    snap = custodian.resume(HOST_ID, EXEC_SHA)
    assert snap["jobs"][0]["state"] == "closed"
    assert refs_used(backend) == {created}
    assert custodian.owned_objects() == ()


# ---- 运行期 Backend 失败与同实例接手 --------------------------------------------------


def test_pump_failure_then_resume_same_ref():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.pump_script[ref] = deque([BackendError(), BackendError(), chunk(b"ok", eof=True)])
    first = custodian.step()["jobs"][0]
    assert first["state"] == "unknown"
    assert first["primary_error"] == "host_backend"
    second = custodian.step()["jobs"][0]
    assert second["state"] == "unknown"
    assert second["primary_error"] == "host_backend"
    assert second["cleanup_errors"] == []
    third = custodian.resume(HOST_ID, EXEC_SHA)["jobs"][0]
    assert third["state"] == "closed"
    assert third["stdout"] == b"ok"
    assert refs_used(backend) == {ref}
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_backend"


def test_exit_code_failure_then_resume():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([BackendError(), 7])
    first = custodian.step()["jobs"][0]
    assert first["state"] == "unknown"
    assert first["primary_error"] == "host_backend"
    second = custodian.step()["jobs"][0]
    assert second["state"] == "closed"
    assert second["returncode"] == 7
    assert second["original_exited"] is True
    assert refs_used(backend) == {ref}
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_backend"


# ---- 时钟 ----------------------------------------------------------------------------


def test_clock_regression_rejected_state_kept():
    custodian, backend, clock = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([None, 0])
    custodian.step()
    clock[0] = 900
    with pytest.raises(HostError) as exc:
        custodian.step()
    assert exc.value.code == "host_clock"
    with pytest.raises(HostError):
        custodian.decision()
    assert custodian.snapshot()["jobs"][0]["state"] == "running"
    clock[0] = 1100
    assert custodian.step()["jobs"][0]["state"] == "closed"


@pytest.mark.parametrize("bad", ["1000", 10.5, True, None, -5])
def test_clock_value_rejected(bad):
    backend = FakeBackend()
    custodian = Custodian(identity(), backend, lambda: bad, 1000, 60)
    with pytest.raises(HostError) as exc:
        custodian.step()
    assert exc.value.code == "host_clock"
    assert backend.calls == []


# ---- 输出超限 ------------------------------------------------------------------------


def test_output_limit_stops_job_keeps_exact_prefix():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.pump_script[ref] = deque([chunk(b"x" * 40000), chunk(b"more", eof=True)])
    backend.exit_script[ref] = deque([None, 0])
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "stopping"
    assert entry["primary_error"] == "host_output_limit"
    assert entry["stdout"] == b"x" * 32768
    assert methods(backend).count("terminate") == 1
    entry = custodian.step()["jobs"][0]
    assert entry["stdout"] == b"x" * 32768
    assert entry["cleanup_errors"] == []
    assert entry["state"] == "closed"


def test_output_limit_after_exit_only_notes():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.pump_script[ref] = deque([chunk(), chunk(b"y" * 40000, eof=True)])
    backend.exit_script[ref] = deque([0])
    custodian.step()
    entry = custodian.step()["jobs"][0]
    assert entry["primary_error"] == "host_output_limit"
    assert methods(backend).count("terminate") == 0
    assert entry["state"] == "closed"


# ---- 关闭计数与引用保留 ------------------------------------------------------------------


def test_close_pipes_failure_single_attempt_keeps_ref():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.errors["close_pipes"] = BackendError()
    snap = custodian.step()
    entry = snap["jobs"][0]
    assert entry["state"] == "unknown"
    assert entry["primary_error"] == "host_cleanup"
    assert entry["close_attempts"] == 2
    assert entry["close_successes"] == 1
    assert snap["custody_required"] is True
    assert custodian.owned_objects() == (ref,)
    calls_before = list(backend.calls)
    snap = custodian.step()
    assert snap["jobs"][0]["state"] == "unknown"
    assert backend.calls == calls_before
    result = custodian.decision()
    assert result["can_exit"] is False
    assert result["success"] is False
    assert result["reason"] == "host_cleanup"


def test_close_handle_failure_single_attempt_keeps_ref():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.errors["close_handle"] = BackendError()
    entry = custodian.step()["jobs"][0]
    assert entry["state"] == "unknown"
    assert entry["primary_error"] == "host_cleanup"
    assert entry["close_attempts"] == 2
    assert entry["close_successes"] == 1
    assert custodian.owned_objects() == (ref,)
    calls_before = list(backend.calls)
    custodian.step()
    assert backend.calls == calls_before


def test_primary_and_cleanup_errors_kept_separately():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.pump_script[ref] = deque([chunk(b"x" * 40000), chunk(eof=True)])
    backend.exit_script[ref] = deque([None, 0])
    backend.errors["close_handle"] = BackendError()
    first = custodian.step()["jobs"][0]
    assert first["primary_error"] == "host_output_limit"
    second = custodian.step()["jobs"][0]
    assert second["primary_error"] == "host_output_limit"
    assert second["cleanup_errors"] == ["host_cleanup"]
    assert second["state"] == "unknown"


# ---- 退出判定、调用计数与引用恒等 ---------------------------------------------------------


def test_nonzero_returncode_can_exit_without_success():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([3])
    assert custodian.step()["jobs"][0]["state"] == "closed"
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_job"


def test_closed_job_no_further_backend_calls():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    custodian.step()
    assert custodian.snapshot()["jobs"][0]["state"] == "closed"
    calls_before = list(backend.calls)
    custodian.step()
    custodian.snapshot()
    custodian.cancel(JOB_ID, "deadline")
    custodian.decision()
    assert backend.calls == calls_before


def test_decision_snapshot_owned_objects_no_backend_calls():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([None])
    custodian.step()
    calls_before = list(backend.calls)
    custodian.snapshot()
    custodian.decision()
    custodian.owned_objects()
    assert backend.calls == calls_before


def test_owned_objects_are_original_refs_whole_lifecycle():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.exit_script[ref] = deque([None, 0])
    assert custodian.owned_objects() == (ref,)
    custodian.step()
    assert custodian.owned_objects() == (ref,)
    custodian.step()
    assert custodian.owned_objects() == ()
    assert refs_used(backend) == {ref}
