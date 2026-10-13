import asyncio
from dataclasses import replace
from pathlib import Path

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import sha256_file
from inferyard.runtime.batch_runner import execute_async
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
    from inferyard.reporting.report import verify_report, write_report

    report = tmp_path / "outcome-report"
    index = write_report([root], report)
    view = index["runs"][0]["scan_view"]
    assert [r["state"] for r in view["outputs"]] == ["completed", "failed", "not_executed"]
    assert view["outputs"][-1]["actual"]["value"] is None
    assert sum(r["planned"] for r in view["length_bins"]) == 3
    assert sum(r["outcomes"]["terminal_counts"]["failed"] for r in view["length_bins"]) == 1
    html = (report / "report.html").read_text()
    assert 'class="length-outcomes"' in html and "500：1" in html
    assert "未测档不等于不支持" in html
    assert verify_report(report)["verified"]
    assert before == {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
