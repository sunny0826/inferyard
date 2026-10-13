"""Custodian 输入校验与正常路径回归(协议 mock,仅 fake Backend,非 Windows 原生证据)。"""

from collections import deque
from copy import deepcopy

import pytest

from scripts.ninfer_source_host.custody import (
    PROTOCOL,
    STAGE,
    Custodian,
    HostError,
)

HOST_ID = "11111111-2222-4333-8444-abcdefabcdef"
JOB_ID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
JOB_ID_2 = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
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


def chunk(stdout=b"", stderr=b"", eof=False):
    return {"stdout": stdout, "stderr": stderr, "eof": eof}


# ---- 构造与 identity 校验 ---------------------------------------------------------


def test_identity_accepted_and_copied():
    source = identity()
    backend = FakeBackend()
    custodian = Custodian(source, backend, lambda: 1000, 1000, 60)
    source["stage"] = 1
    source["host_id"] = JOB_ID
    assert custodian.snapshot()["stage"] == STAGE
    assert custodian.snapshot()["host_id"] == HOST_ID


@pytest.mark.parametrize(
    "patch",
    [
        {"protocol": "other/1"},
        {"protocol": 45},
        {"host_id": HOST_ID.upper()},
        {"host_id": HOST_ID.replace("-", "_")},
        {"host_id": 123},
        {"execution_sha256": "A" * 64},
        {"execution_sha256": "a" * 63},
        {"execution_sha256": 123},
        {"scripts_sha256": "B" * 64},
        {"stage": True},
        {"stage": 44},
        {"stage": "45"},
        {"stage": 45.0},
        {"work_end_ticks": -1},
        {"work_end_ticks": True},
        {"work_end_ticks": "1740"},
        {"total_end_ticks": True},
        {"total_end_ticks": 1740},
        {"total_end_ticks": 1700},
    ],
)
def test_identity_field_rejected(patch):
    with pytest.raises(HostError) as exc:
        Custodian(identity(**patch), FakeBackend(), lambda: 1000, 1000, 60)
    assert exc.value.code == "host_identity"


@pytest.mark.parametrize(
    "bad",
    [
        "not-a-dict",
        [1, 2],
        {**identity(), "extra": 1},
        {key: value for key, value in identity().items() if key != "stage"},
    ],
)
def test_identity_shape_rejected(bad):
    with pytest.raises(HostError) as exc:
        Custodian(bad, FakeBackend(), lambda: 1000, 1000, 60)
    assert exc.value.code == "host_identity"


@pytest.mark.parametrize("frequency", [0, -1, True, "1000", 10.5])
def test_frequency_rejected(frequency):
    with pytest.raises(HostError) as exc:
        Custodian(identity(), FakeBackend(), lambda: 1000, frequency, 60)
    assert exc.value.code == "host_clock"


@pytest.mark.parametrize("grace", [-1, True, "60", 1.5])
def test_grace_rejected(grace):
    with pytest.raises(HostError) as exc:
        Custodian(identity(), FakeBackend(), lambda: 1000, 1000, grace)
    assert exc.value.code == "host_clock"


def test_now_ticks_not_callable_rejected():
    with pytest.raises(HostError) as exc:
        Custodian(identity(), FakeBackend(), 1000, 1000, 60)
    assert exc.value.code == "host_clock"


def test_inputs_not_mutated():
    ident, jb, sp = identity(), job(), spec()
    ident_copy, job_copy, spec_copy = deepcopy(ident), deepcopy(jb), deepcopy(sp)
    custodian = Custodian(ident, FakeBackend(), lambda: 1000, 1000, 60)
    custodian.start(jb, sp)
    custodian.step()
    assert ident == ident_copy
    assert jb == job_copy
    assert sp == spec_copy


# ---- 正常生命周期 -----------------------------------------------------------------


def test_single_step_closed_success():
    custodian, backend, _ = make()
    assert custodian.start(job(), spec()) == JOB_ID
    snap = custodian.step()
    entry = snap["jobs"][0]
    assert entry["state"] == "closed"
    assert entry["returncode"] == 0
    assert entry["original_exited"] is True
    assert entry["close_attempts"] == 2
    assert entry["close_successes"] == 2
    assert entry["primary_error"] is None
    assert entry["cleanup_errors"] == []
    assert snap["custody_required"] is False
    assert custodian.owned_objects() == ()
    result = custodian.decision()
    assert result["can_exit"] is True
    assert result["success"] is True
    assert result["reason"] is None


def test_output_accumulates_across_steps():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.pump_script[ref] = deque([chunk(b"ab", b"x"), chunk(b"cd", b"y", eof=True)])
    backend.exit_script[ref] = deque([None, 0])
    first = custodian.step()["jobs"][0]
    assert first["state"] == "running"
    assert first["stdout"] == b"ab"
    assert first["returncode"] is None
    second = custodian.step()["jobs"][0]
    assert second["state"] == "closed"
    assert second["stdout"] == b"abcd"
    assert second["stderr"] == b"xy"
    assert second["returncode"] == 0


def test_snapshot_exact_keys_and_independence():
    custodian, _, _ = make()
    custodian.start(job(), spec())
    snap = custodian.snapshot()
    assert set(snap) == {
        "protocol",
        "host_id",
        "execution_sha256",
        "scripts_sha256",
        "stage",
        "jobs",
        "custody_required",
    }
    entry = snap["jobs"][0]
    assert set(entry) == {
        "job_id",
        "kind",
        "state",
        "returncode",
        "original_exited",
        "close_attempts",
        "close_successes",
        "stdout",
        "stderr",
        "primary_error",
        "cleanup_errors",
    }
    entry["state"] = "tampered"
    entry["cleanup_errors"].append("tampered")
    entry["stdout"] = b"tampered"
    snap["jobs"].append({})
    again = custodian.snapshot()
    assert len(again["jobs"]) == 1
    assert again["jobs"][0]["state"] == "running"
    assert again["jobs"][0]["cleanup_errors"] == []
    assert again["jobs"][0]["stdout"] == b""


def test_zero_job_decision():
    custodian, _, _ = make()
    result = custodian.decision()
    assert result["jobs"] == []
    assert result["can_exit"] is True
    assert result["success"] is False
    assert result["reason"] == "host_job"
    assert result["custody_required"] is False


# ---- start 拒绝(全部零 Backend 调用) ----------------------------------------------


@pytest.mark.parametrize(
    "patch, code",
    [
        ({"job_id": JOB_ID.upper()}, "host_job"),
        ({"job_id": "not-a-uuid"}, "host_job"),
        ({"kind": "download"}, "host_job"),
        ({"kind": True}, "host_job"),
        ({"task_end_ticks": 1000}, "host_job"),
        ({"task_end_ticks": 999}, "host_job"),
        ({"task_end_ticks": 1741}, "host_job"),
        ({"task_end_ticks": True}, "host_job"),
        ({"task_end_ticks": "1500"}, "host_job"),
        ({"protocol": "other/1"}, "host_identity"),
        ({"stage": True}, "host_identity"),
        ({"host_id": JOB_ID}, "host_identity"),
        ({"work_end_ticks": 9999}, "host_identity"),
        ({"total_end_ticks": 9999}, "host_identity"),
        ({"execution_sha256": "f" * 64}, "host_identity"),
        ({"scripts_sha256": "f" * 64}, "host_identity"),
    ],
)
def test_start_job_field_rejected(patch, code):
    custodian, backend, _ = make()
    with pytest.raises(HostError) as exc:
        custodian.start(job(**patch), spec())
    assert exc.value.code == code
    assert backend.calls == []


@pytest.mark.parametrize(
    "bad",
    [
        "not-a-dict",
        [1],
        {**job(), "extra": 1},
        {key: value for key, value in job().items() if key != "kind"},
    ],
)
def test_start_job_shape_rejected(bad):
    custodian, backend, _ = make()
    with pytest.raises(HostError) as exc:
        custodian.start(bad, spec())
    assert exc.value.code == "host_job"
    assert backend.calls == []


@pytest.mark.parametrize(
    "patch",
    [
        {"argv": ["python", "worker.py"]},
        {"argv": ()},
        {"argv": ("python", 3)},
        {"argv": ("py\0thon",)},
        {"argv": ("x" * 12001,)},
        {"cwd": "relative/path"},
        {"cwd": ""},
        {"cwd": 123},
        {"executable_sha256": "C" * 64},
        {"executable_sha256": 123},
        {"script_sha256": "d" * 63},
        {"no_descendants": False},
        {"no_descendants": 1},
    ],
)
def test_start_spec_field_rejected(patch):
    custodian, backend, _ = make()
    with pytest.raises(HostError) as exc:
        custodian.start(job(), spec(**patch))
    assert exc.value.code == "host_job"
    assert backend.calls == []


@pytest.mark.parametrize(
    "bad",
    [
        "not-a-dict",
        {**spec(), "extra": 1},
        {key: value for key, value in spec().items() if key != "cwd"},
    ],
)
def test_start_spec_shape_rejected(bad):
    custodian, backend, _ = make()
    with pytest.raises(HostError) as exc:
        custodian.start(job(), bad)
    assert exc.value.code == "host_job"
    assert backend.calls == []


def test_job_id_never_reused_after_close():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    custodian.step()
    assert custodian.snapshot()["jobs"][0]["state"] == "closed"
    with pytest.raises(HostError) as exc:
        custodian.start(job(), spec())
    assert exc.value.code == "host_job"
    assert methods(backend).count("start") == 1


def test_busy_while_job_open():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    with pytest.raises(HostError) as exc:
        custodian.start(job(job_id=JOB_ID_2), spec())
    assert exc.value.code == "host_busy"
    assert methods(backend).count("start") == 1


# ---- cancel 与 resume ---------------------------------------------------------------


@pytest.mark.parametrize("reason", [[], "bad", None, 3, True])
def test_cancel_reason_rejected(reason):
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    calls_before = list(backend.calls)
    with pytest.raises(HostError) as exc:
        custodian.cancel(JOB_ID, reason)
    assert exc.value.code == "host_job"
    assert backend.calls == calls_before


@pytest.mark.parametrize("bad_id", [JOB_ID_2, 123, None, []])
def test_cancel_unknown_job_rejected(bad_id):
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    calls_before = list(backend.calls)
    with pytest.raises(HostError) as exc:
        custodian.cancel(bad_id, "operator_cancel")
    assert exc.value.code == "host_job"
    assert backend.calls == calls_before


def test_cancel_running_job_stops_once():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.pump_script[ref] = deque([chunk()])
    backend.exit_script[ref] = deque([None, 0])
    custodian.step()
    snap = custodian.cancel(JOB_ID, "operator_cancel")
    entry = snap["jobs"][0]
    assert entry["state"] == "stopping"
    assert entry["primary_error"] == "host_cancelled"
    assert methods(backend).count("terminate") == 1
    custodian.step()
    assert custodian.snapshot()["jobs"][0]["state"] == "closed"
    assert methods(backend).count("terminate") == 1
    assert methods(backend).count("kill") == 0


def test_cancel_closed_job_is_noop():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    custodian.step()
    assert custodian.snapshot()["jobs"][0]["state"] == "closed"
    calls_before = list(backend.calls)
    snap = custodian.cancel(JOB_ID, "io_error")
    assert snap["jobs"][0]["state"] == "closed"
    assert backend.calls == calls_before


@pytest.mark.parametrize(
    "host_id, sha",
    [
        (JOB_ID, EXEC_SHA),
        (HOST_ID, "f" * 64),
        ([], EXEC_SHA),
        (HOST_ID, {}),
        (None, EXEC_SHA),
    ],
)
def test_resume_identity_rejected(host_id, sha):
    custodian, backend, _ = make()
    with pytest.raises(HostError) as exc:
        custodian.resume(host_id, sha)
    assert exc.value.code == "host_identity"
    assert backend.calls == []


def test_resume_matching_identity_steps():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    snap = custodian.resume(HOST_ID, EXEC_SHA)
    assert snap["jobs"][0]["state"] == "closed"
    assert ("pump", backend.started[0]) in backend.calls
