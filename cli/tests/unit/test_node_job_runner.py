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


def test_legacy_string_error_receipt_is_migrated_on_load(tmp_path):
    for job_id, legacy_status, expected in (
        ("legacy-1", "error", "failed"),
        ("legacy-2", "blocked", "failed"),
        ("legacy-3", "finished", "done"),
    ):
        job_dir = tmp_path / "jobs" / job_id
        job_dir.mkdir(parents=True)
        (job_dir / "job.json").write_text(
            json.dumps({"job_id": job_id, "status": legacy_status, "error": "RuntimeError"}) + "\n",
            encoding="utf-8",
        )
    runner = JobRunner(tmp_path, Supervisor())
    assert runner.has_active_jobs() is False
    loaded = runner.progress("legacy-1")
    assert loaded["task_id"] == "legacy-1"
    assert loaded["state"] == loaded["status"] == "failed"
    assert isinstance(loaded["error"], dict) and loaded["error"]["message"] == "RuntimeError"
    assert runner.progress("legacy-2")["state"] == "failed"
    assert runner.progress("legacy-3")["state"] == "done"


def test_rerun_prepares_service_binding_from_trial_evidence_inputs_layout(
    tmp_path, config_path, monkeypatch
):
    from inferyard.analysis.scoring import ScoringContext
    from inferyard.config.single_plan import compile_single_plan
    from inferyard.evidence.journal import TrialJournal

    loaded = load_config(str(config_path))
    config = loaded.config.to_dict()
    bundle = loaded.bundle.to_dict()
    config["definition"] = "local-engine-config.v1"
    config["model"] = {
        "display_name": "Local model",
        "repo": "local",
        "revision": "frozen",
        "kind": "directory",
        "local_path": "model",
        "packing": "4bit",
        "template_path": "template.jinja",
        "template_sha256": "3" * 64,
    }
    config["engine"] = {
        "adapter": "local_openai_v1",
        "family": "mlx-lm",
        "backend": "metal",
        "installation_path": "runtime.json",
        "installation_sha256": "4" * 64,
        "startup_args": [
            "-I",
            "-B",
            "/installed/mlx_server.py",
            "--host",
            "127.0.0.1",
            "--port",
            "48850",
        ],
        "working_directory": "/runtime",
        "environment": {"PYTHONDONTWRITEBYTECODE": "1"},
        "served_model": "formal-local",
    }
    config["endpoint"].update(url="http://127.0.0.1:48850", server_pid=100)
    config["generation"] = {"temperature": 0, "max_tokens": 512}
    for field in ("threads", "threads_batch", "slots"):
        config["conditions"].pop(field, None)
    config["conditions"].update(cache_policy="unknown", ac_online=True, model_loaded=True)
    config["safety"] = {"interval_seconds": 0.5, "check_environment": True}
    results_root = tmp_path / "results"
    config["output"]["root"] = str(results_root)
    plan = compile_single_plan(config, bundle)
    store = TrialJournal(
        results_root,
        plan,
        plan["trials"][0]["trial_id"],
        config,
        bundle,
        execution_mode="single",
        scoring_context=ScoringContext(),
    )
    from_run = store.path
    store.close()
    assert (from_run / "inputs.json").is_file()

    bound_configs = []
    monkeypatch.setattr(
        "inferyard.platforms.local_engines.service.bind_service",
        lambda cfg: bound_configs.append(cfg) or {"definition": "local-engine-binding.v1"},
    )
    monkeypatch.setattr("inferyard.platforms.identity.process_start_ticks", lambda pid: 999)

    seen = []

    async def executor(request):
        seen.append(request)
        return 0, CommandResult("run", "finished")

    class ActiveSupervisor:
        def list(self):
            return [{"service_id": "svc-1", "status": "running"}]

        def endpoint(self, service_id):
            return {"url": "http://127.0.0.1:48857", "server_pid": 4321, "process_start_ticks": 999}

    async def scenario():
        runner = JobRunner(tmp_path, ActiveSupervisor(), executor=executor)
        await runner.start_request({"action": "rerun", "remote_run_paths": [str(from_run)]})
        await asyncio.gather(*runner.tasks.values())
        assert len(bound_configs) == 1
        assert bound_configs[0]["endpoint"]["server_pid"] == 4321
        assert bound_configs[0]["engine"]["startup_args"][-4:] == [
            "--host",
            "127.0.0.1",
            "--port",
            "48857",
        ]
        assert seen[0].service_binding_path is not None and seen[0].service_binding_path.is_file()
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


def test_multi_config_validation_failure_starts_zero_jobs(tmp_path, config_path):
    started = []

    async def executor(request):
        started.append(request)
        return 0, CommandResult("run", "finished")

    async def scenario():
        runner = JobRunner(tmp_path, Supervisor(), executor=executor)
        with pytest.raises((OSError, ValueError)):
            await runner.start_request(
                {"action": "start", "config_paths": [str(config_path), str(tmp_path / "missing.toml")]}
            )
        assert runner.list() == []
        assert started == []
        assert list((tmp_path / "jobs").iterdir()) == []
        await runner.close()

    asyncio.run(scenario())


def test_restarted_runner_reconciles_interrupted_running_job(tmp_path):
    job_dir = tmp_path / "jobs" / "orphan-run"
    job_dir.mkdir(parents=True)
    (job_dir / "job.json").write_text(
        json.dumps({"job_id": "orphan-run", "status": "running", "state": "running"}) + "\n",
        encoding="utf-8",
    )
    runner = JobRunner(tmp_path, Supervisor())
    loaded = runner.progress("orphan-run")
    assert loaded["state"] == loaded["status"] == "failed"
    assert loaded["error"]["code"] == "agent_restarted"
