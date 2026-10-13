"""Merged host libraries with fake worker I/O and WinAPI; no native qualification."""

import base64
import json

import pytest

from scripts.ninfer_source_host.custody import Custodian
from scripts.ninfer_source_host.file_io import execute_io, main
from scripts.ninfer_source_host.windows_backend import WindowsBackend
from tests.unit.test_ninfer_source_backend import FakeApi, spec
from tests.unit.test_ninfer_source_file_io import Clock, Mem, request


def owner_for(value, api, clock, monkeypatch):
    identity = {
        key: value[key]
        for key in ("protocol", "host_id", "execution_sha256", "scripts_sha256", "stage")
    } | {"work_end_ticks": 55, "total_end_ticks": value["total_end_ticks"]}
    job = identity | {
        "job_id": value["job_id"],
        "kind": "file_io",
        "task_end_ticks": value["task_end_ticks"],
    }
    # Only the injected Windows-shaped cwd is accepted on the Mac test host.
    monkeypatch.setattr(
        "scripts.ninfer_source_host.custody.os.path.isabs", lambda path: path == spec()["cwd"]
    )
    owner = Custodian(identity, WindowsBackend(api=api), clock, 1, 2)
    owner.start(job, spec())
    return owner, identity


def deliver_result(api, result):
    frame = json.dumps(result, ensure_ascii=False, separators=(",", ":")).encode() + b"\n"
    api.stdout.data, api.stdout.eof = frame, True
    api.stderr.eof = True
    api.exit_value = 0 if result["primary_error"] is None else 2
    return frame


def assert_closed_once(owner, api):
    assert owner.owned_objects() == ()
    assert api.closed == [api.stdout, api.stderr, api.process, api.thread, api.job]
    snapshot = owner.snapshot()["jobs"][0]
    assert snapshot["original_exited"] is True
    assert snapshot["close_attempts"] == snapshot["close_successes"] == 2
    before = (len(api.peeked), len(api.read_sizes), len(api.closed))
    owner.step()
    assert before == (len(api.peeked), len(api.read_sizes), len(api.closed))


@pytest.mark.parametrize("operation", ["read", "hash", "write_new"])
def test_io_result_survives_backend_and_same_owner_resume(operation, monkeypatch):
    payload = "不同分块与普通文件\n".encode()
    value = request(operation, payload=payload)
    io = Mem({value["path"]: payload}) if operation != "write_new" else Mem()
    clock, api = Clock(), FakeApi()
    owner, identity = owner_for(value, api, clock, monkeypatch)
    ref = owner.owned_objects()[0]
    result = execute_io(value, io_api=io, now_ticks=clock)
    expected = deliver_result(api, result)
    owner.step()
    assert owner.owned_objects() == (ref,)
    assert owner.decision()["can_exit"] is False
    api.wait_result = 0
    owner.resume(identity["host_id"], identity["execution_sha256"])
    snapshot = owner.snapshot()["jobs"][0]
    assert snapshot["stdout"] == expected
    received = json.loads(snapshot["stdout"])
    for key in ("protocol", "host_id", "job_id", "execution_sha256", "scripts_sha256", "stage"):
        assert received[key] == value[key]
    assert received["sha256"] == value["expected_sha256"]
    assert received["bytes"] == len(payload)
    assert received["primary_error"] is None
    if operation == "read":
        assert base64.b64decode(received["data_b64"], validate=True) == payload
    else:
        assert received["data_b64"] is None
    assert owner.decision()["success"] is True
    assert_closed_once(owner, api)


def test_invalid_path_worker_result_is_preserved_and_denies_success(monkeypatch):
    value = request("read", path="C:\\lab\\COM¹.txt")
    io, clock, api = Mem(), Clock(), FakeApi()
    owner, _identity = owner_for(value, api, clock, monkeypatch)
    result = execute_io(value, io_api=io, now_ticks=clock)
    expected = deliver_result(api, result)
    assert result["primary_error"] == "io_path" and io.calls == []
    api.wait_result = 0
    owner.step()
    assert owner.snapshot()["jobs"][0]["stdout"] == expected
    assert owner.decision()["can_exit"] is True
    assert owner.decision()["success"] is False
    assert_closed_once(owner, api)


def test_worker_close_at_task_deadline_cannot_become_host_success(monkeypatch):
    value = request("read")
    clock, api = Clock(), FakeApi()
    owner, _identity = owner_for(value, api, clock, monkeypatch)
    io = Mem({value["path"]: b"hi"}, clock=clock, expire="close")
    result = execute_io(value, io_api=io, now_ticks=clock)
    assert clock.t == value["task_end_ticks"]
    assert result["primary_error"] == "io_deadline"
    assert result["data_b64"] is None and io.closed == [1]
    expected = deliver_result(api, result)
    api.wait_result = 0
    owner.step()
    assert owner.snapshot()["jobs"][0]["stdout"] == expected
    assert owner.snapshot()["jobs"][0]["primary_error"] == "host_deadline"
    assert owner.decision()["success"] is False
    assert_closed_once(owner, api)


def test_cancel_then_resume_drains_original_backend_ref(monkeypatch):
    value = request("read")
    clock, api = Clock(), FakeApi()
    owner, identity = owner_for(value, api, clock, monkeypatch)
    ref = owner.owned_objects()[0]
    owner.cancel(value["job_id"], "operator_cancel")
    assert api.terminated == [api.process]
    assert owner.owned_objects() == (ref,)
    api.stdout.data, api.stdout.eof = b"tail-after-cancel", True
    api.stderr.eof, api.wait_result, api.exit_value = True, 0, 2
    clock.t += 1
    owner.resume(identity["host_id"], identity["execution_sha256"])
    assert owner.snapshot()["jobs"][0]["stdout"] == b"tail-after-cancel"
    assert owner.decision()["success"] is False
    assert api.terminated == [api.process] and api.killed == []
    assert_closed_once(owner, api)


def test_actual_worker_cli_remains_disabled(monkeypatch):
    def forbidden(*_args, **_kwargs):
        raise AssertionError("actual worker I/O must remain disabled")

    monkeypatch.setattr("scripts.ninfer_source_host.file_io.execute_io", forbidden)
    encoded = base64.b64encode(json.dumps(request("read")).encode()).decode()
    assert main(["worker", encoded]) == 2
