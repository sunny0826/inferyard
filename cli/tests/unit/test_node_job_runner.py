import asyncio
import json

from inferyard.application.types import CommandResult
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


def test_cancel_timeout_marks_forced_termination(tmp_path, config_path, monkeypatch):
    killed = []

    async def executor(request):
        try:
            await asyncio.sleep(60)
        except asyncio.CancelledError:
            await asyncio.sleep(60)

    monkeypatch.setattr(
        "inferyard.node.job_runner.terminate_tree",
        lambda pid, ticks, graceful_timeout=0: killed.append((pid, ticks)) or "drain_timeout",
    )

    async def scenario():
        runner = JobRunner(tmp_path, Supervisor(), executor=executor)
        record = await runner.start({"job_id": "job-2", "config": str(config_path)})
        await asyncio.sleep(0)
        result = await runner.cancel("job-2", drain_timeout=.001)
        assert result["status"] == "cancelled"
        assert result["forced_termination_reason"] == "drain_timeout"
        assert killed == [(record["worker_pid"], record["worker_start_ticks"])]
        await runner.close()

    asyncio.run(scenario())
