"""Custodian 任务拒绝、输出和取消回归；fake Backend 不构成原生证据。"""

from collections import deque

import pytest

from scripts.ninfer_source_host.custody import PROTOCOL, STAGE, Custodian, HostError

HOST_ID = "11111111-2222-4333-8444-abcdefabcdef"
JOB_ID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
JOB_ID_2 = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
EXEC_SHA = "a" * 64
SCRIPTS_SHA = "b" * 64
EXE_SHA = "c" * 64
SCRIPT_SHA = "d" * 64


def identity(**over):
    return {
        "protocol": PROTOCOL,
        "host_id": HOST_ID,
        "execution_sha256": EXEC_SHA,
        "scripts_sha256": SCRIPTS_SHA,
        "stage": STAGE,
        "work_end_ticks": 1740,
        "total_end_ticks": 1800,
        **over,
    }


def job(**over):
    return {**identity(), "job_id": JOB_ID, "kind": "file_io", "task_end_ticks": 1500, **over}


def spec(**over):
    return {
        "argv": ("python", "worker.py"),
        "cwd": "/tmp/work",
        "executable_sha256": EXE_SHA,
        "script_sha256": SCRIPT_SHA,
        "no_descendants": True,
        **over,
    }


class FakeBackend:
    def __init__(self):
        self.calls = []
        self.started = []
        self.pump_script = {}
        self.exit_script = {}

    def start(self, spec_value):
        ref = object()
        self.started.append(ref)
        self.calls.append(("start", ref))
        return ref

    def _scripted(self, script, ref, default):
        queue = script.get(ref)
        return queue.popleft() if queue else default

    def pump(self, ref, limit):
        self.calls.append(("pump", ref))
        return self._scripted(self.pump_script, ref, {"stdout": b"", "stderr": b"", "eof": True})

    def exit_code(self, ref):
        self.calls.append(("exit_code", ref))
        return self._scripted(self.exit_script, ref, 0)

    def terminate(self, ref):
        self.calls.append(("terminate", ref))

    def kill(self, ref):
        self.calls.append(("kill", ref))

    def close_pipes(self, ref):
        self.calls.append(("close_pipes", ref))

    def close_handle(self, ref):
        self.calls.append(("close_handle", ref))


def make(clock_start=1000):
    clock = [clock_start]
    backend = FakeBackend()
    custodian = Custodian(identity(), backend, lambda: clock[0], 1000, 60)
    return custodian, backend, clock


def methods(backend):
    return [method for method, _ in backend.calls]


def chunk(stdout=b"", stderr=b"", eof=False):
    return {"stdout": stdout, "stderr": stderr, "eof": eof}


def test_single_step_closed_success():
    custodian, _, _ = make()
    custodian.start(job(), spec())
    custodian.step()
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


def test_job_id_never_reused_after_close():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    custodian.step()
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


def test_cancel_running_job_stops_once():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    ref = backend.started[0]
    backend.pump_script[ref] = deque([chunk()])
    backend.exit_script[ref] = deque([None, 0])
    custodian.step()
    entry = custodian.cancel(JOB_ID, "operator_cancel")["jobs"][0]
    assert entry["state"] == "stopping"
    assert entry["primary_error"] == "host_cancelled"
    custodian.step()
    assert custodian.snapshot()["jobs"][0]["state"] == "closed"
    assert methods(backend).count("terminate") == 1
    assert methods(backend).count("kill") == 0


def test_cancel_closed_job_is_noop():
    custodian, backend, _ = make()
    custodian.start(job(), spec())
    custodian.step()
    calls_before = list(backend.calls)
    snap = custodian.cancel(JOB_ID, "io_error")
    assert snap["jobs"][0]["state"] == "closed"
    assert backend.calls == calls_before


@pytest.mark.parametrize(
    "host_id, sha",
    [(JOB_ID, EXEC_SHA), (HOST_ID, "f" * 64), ([], EXEC_SHA), (HOST_ID, {}), (None, EXEC_SHA)],
)
def test_resume_identity_rejected(host_id, sha):
    custodian, backend, _ = make()
    with pytest.raises(HostError) as exc:
        custodian.resume(host_id, sha)
    assert exc.value.code == "host_identity"
    assert backend.calls == []
