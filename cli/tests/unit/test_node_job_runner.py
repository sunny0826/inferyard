import asyncio
import json
from dataclasses import replace

import pytest

from inferyard.application.types import CommandResult
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document
from inferyard.node.job_runner import JobRunner


class Supervisor:
    def endpoint(self, service_id):
        raise AssertionError("not used")


def test_cooperative_cancel_writes_marker_and_seals(tmp_path, config_path):
    sealed = tmp_path / "sealed.json"

    async def executor(request):
        assert request.config.config.to_dict()["output"]["root"].endswith("results")
        try:
            await asyncio.sleep(60)
        except asyncio.CancelledError:
            sealed.write_text('{"status":"sealed"}')
            return 130, CommandResult("run", "interrupted", evidence_dir=str(sealed.parent))
        raise AssertionError("unreachable")

    async def scenario():
        runner = JobRunner(tmp_path, Supervisor(), executor=executor)
        record = await runner.start({"job_id": "job-1", "config": str(config_path)})
        await asyncio.sleep(0)
        assert record["status"] == "running"
        result = await runner.cancel("job-1", drain_timeout=1)
        assert result["status"] == "cancelled"
        assert result["code"] == 130
        assert sealed.exists()
        marker = json.loads((tmp_path / "jobs" / "job-1" / "cancel.json").read_text())
        assert marker["reason"] == "user_cancelled"
        assert runner.progress("job-1")["cancel_requested"] is True
        await runner.close()

    asyncio.run(scenario())


def test_cancel_timeout_never_terminates_model_service(tmp_path, config_path, monkeypatch):
    killed = []

    async def executor(request):
        try:
            await asyncio.sleep(60)
        except asyncio.CancelledError:
            await asyncio.sleep(60)

    monkeypatch.setattr(
        "inferyard.node.job_runner.terminate_tree",
        lambda pid, ticks, graceful_timeout=0: killed.append((pid, ticks)) or "drain_timeout",
        raising=False,
    )

    async def scenario():
        runner = JobRunner(tmp_path, Supervisor(), executor=executor)
        record = await runner.start({"job_id": "job-2", "config": str(config_path)})
        await asyncio.sleep(0)
        result = await runner.cancel("job-2", drain_timeout=.001)
        assert result["state"] == result["status"] == "cancelled"
        assert result["forced_termination_reason"] == "cooperative_drain_timeout"
        assert record["task_id"] == record["job_id"]
        assert record["worker_pid"] is None
        assert killed == []
        await runner.close()

    asyncio.run(scenario())


def test_go_batch_jobs_and_receipt_fields(tmp_path, config_path):
    requests = []
    active = 0
    max_active = 0

    async def executor(request):
        nonlocal active, max_active
        active += 1
        max_active = max(max_active, active)
        await asyncio.sleep(0.01)
        requests.append(request)
        active -= 1
        return 0, CommandResult("run", "finished", run_id="run-1", evidence_dir="evidence")

    async def scenario():
        runner = JobRunner(tmp_path, Supervisor(), executor=executor)
        result = await runner.start_request(
            {"action": "start", "config_paths": [str(config_path), str(config_path)]}
        )
        assert len(result["jobs"]) == 2
        for record in result["jobs"]:
            assert record["task_id"] == record["job_id"]
            assert record["state"] == record["status"] == "running"
            assert record["created_at"]
            assert record["finished_at"] is None
        await asyncio.gather(*runner.tasks.values())
        assert max_active == 1
        assert len(requests) == 2
        assert requests[0].output_root != requests[1].output_root
        for record in runner.list():
            assert record["state"] == record["status"] == "done"
            assert record["finished_at"] is not None
            assert record["run_path"] == "evidence"
        await runner.close()

    asyncio.run(scenario())


def test_job_failure_records_structured_error_and_rerun_resolves_service(tmp_path, config_path):
    class ActiveSupervisor:
        def list(self):
            return [{"service_id": "svc-1", "status": "running"}]

        def endpoint(self, service_id):
            return {"url": "http://127.0.0.1:48857", "server_pid": 4321}

    seen = []

    async def failing_executor(request):
        seen.append(request)
        if request.from_run is None:
            raise RuntimeError("boom")
        return 0, CommandResult("run", "finished")

    async def scenario():
        runner = JobRunner(tmp_path, ActiveSupervisor(), executor=failing_executor)
        await runner.start({"job_id": "fail-1", "config": str(config_path)})
        await runner.start_request({"action": "rerun", "remote_run_paths": ["/runs/r1"]})
        await asyncio.gather(*runner.tasks.values())
        failed = runner.progress("fail-1")
        assert failed["state"] == "failed"
        assert isinstance(failed["error"], dict) and failed["error"]["code"] == "job_failed"
        assert seen[1].server_pid == 4321 and seen[1].endpoint_url == "http://127.0.0.1:48857"
        await runner.close()

    asyncio.run(scenario())


def test_go_resume_and_rerun_requests(tmp_path):
    requests = []

    async def executor(request):
        requests.append(request)
        return 0, CommandResult(request.command, "finished")

    async def scenario():
        runner = JobRunner(tmp_path, Supervisor(), executor=executor)
        resumed = await runner.start_request(
            {
                "action": "resume",
                "from_run": "/runs/batch/runs/run-a",
                "endpoint_url": "http://127.0.0.1:48857",
                "server_pid": 321,
            }
        )
        assert resumed["task_id"] == resumed["job_id"]
        rerun = await runner.start_request(
            {
                "action": "rerun",
                "remote_run_paths": ["/runs/run-a", "/runs/run-b"],
                "endpoint_url": "http://127.0.0.1:48857",
                "server_pid": 321,
            }
        )
        assert len(rerun["jobs"]) == 2
        await asyncio.gather(*runner.tasks.values())
        assert requests[0].command == "resume"
        assert requests[0].from_run.name == "run-a"
        assert requests[0].config is None
        assert [request.command for request in requests[1:]] == ["run", "run"]
        assert [request.from_run.name for request in requests[1:]] == ["run-a", "run-b"]
        assert all(request.server_pid == 321 for request in requests)
        await runner.close()

    asyncio.run(scenario())


class BoundSupervisor:
    def endpoint(self, service_id):
        assert service_id == "service-1"
        return {
            "url": "http://127.0.0.1:48857",
            "server_pid": 321,
            "process_start_ticks": 777,
        }


@pytest.mark.parametrize(
    ("url", "service_id", "message"),
    [
        ("http://192.168.1.20:48857", None, "loopback"),
        ("http://127.0.0.1:49999", "service-1", "binding"),
    ],
)
def test_frozen_plan_requires_loopback_and_expected_service(
    tmp_path, config_path, monkeypatch, url, service_id, message
):
    loaded = load_config(str(config_path))
    config = loaded.config.to_dict()
    config["endpoint"].update(
        url=url, server_pid=999, process_start_ticks=888
    )
    frozen_loaded = replace(loaded, config=Document.parse("config", config))
    monkeypatch.setattr(
        "inferyard.config.plan_inputs.read_frozen_plan",
        lambda path: ({}, {"workload-1": frozen_loaded}),
    )

    async def executor(request):
        return 0, CommandResult("run", "finished")

    async def scenario():
        runner = JobRunner(tmp_path, BoundSupervisor(), executor=executor)
        params = {"config": str(config_path), "frozen_plan": "plan.json"}
        if service_id:
            params["service_id"] = service_id
        try:
            with pytest.raises(ValueError, match=message):
                await runner.start(params)
        finally:
            await runner.close()

    asyncio.run(scenario())
