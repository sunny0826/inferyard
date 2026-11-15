import asyncio
from dataclasses import replace
from pathlib import Path

from inferyard.analysis.ledger import read_trial
from inferyard.evidence.storage import sha256_file
from inferyard.application.execution import execute_async
from tests.integration.test_batch_runner import batch
from tests.integration.test_runner import scenario

__all__ = ["scenario"]


def test_capacity_stop_preserves_failure_and_forbids_resume_or_next_repeat(tmp_path, scenario):
    request, deps = batch(tmp_path, scenario, capacity=True)
    scenario[3]["fail_index"] = 6
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 3
    assert result.details["last_stop_reason"] == "capacity_scan_failed_request"
    assert len(result.details["runs"]) == 1 and len(scenario[2]) == 7
    root = Path(result.details["runs"][0])
    data = read_trial(root)
    assert [r["execution_state"] for r in data["requests"]] == [
        "completed",
        "failed",
        "not_executed",
    ]
    assert data["requests"][1]["http_response"] == {"status_code": 500}
    before = {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
    for command in (request, replace(request, command="resume", from_run=root)):
        code, retried = asyncio.run(execute_async(command, deps))
        assert code == 3 and retried.status == "capacity_scan_stopped"
        assert len(scenario[2]) == 7
    from inferyard.reporting.studio_report import write_studio_report

    report = tmp_path / "outcome-report"
    document = write_studio_report([root], report)
    run = document["runs"][0]
    assert [row["state"] for row in run["requests"]] == [
        "completed",
        "failed",
        "not_executed",
    ]
    assert run["summary"]["completion_rate"] == {
        "numerator": 1,
        "denominator": 2,
        "excluded": 1,
    }
    assert before == {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
