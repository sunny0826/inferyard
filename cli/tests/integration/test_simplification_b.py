"""B live failure boundaries using the existing simulated service."""

import asyncio
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.analysis.scoring import score_case
from inferyard.application.types import CommandRequest
from inferyard.contracts.validation import Document
from inferyard.errors import EvidenceError, PreflightError
from inferyard.analysis.ledger import read_trial
from inferyard.evidence.storage import read_json
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.application.execution import execute_async
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs

scenario = runner_scenario


def test_isolated_score_failure_collects_following_answers(scenario):
    request, deps, calls, _ = scenario
    count = 0

    def score(case, answer, policy):
        nonlocal count
        count += 1
        if count == 1:
            raise RuntimeError("private response must not escape")
        return score_case(case, answer, policy)

    deps.scorer = score
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 3
    data = read_trial(Path(result.evidence_dir))
    assert len(calls) == 8
    assert data["summary"]["counts"]["completed"] == 3
    assert data["summary"]["counts"]["not_executed"] == 0
    assert data["summary"]["scope_complete"]
    assert data["summary"]["completeness"] == "incomplete"
    assert data["requests"][0]["score"]["reason"] == "scorer_exception"
    assert all(r["score"]["quality_state"] in ("pass", "fail") for r in data["requests"][1:])
    assert "private response" not in str(data)
    assert all(g["rate"]["value"] is None for g in data["summary"]["quality"]["Q01"].values())


@pytest.mark.parametrize("dirty,fresh", [(False, False), (True, False), (False, True)])
def test_rerun_same_service_is_always_refused(scenario, monkeypatch, dirty, fresh):
    import inferyard.runtime.lock as locking

    request, deps, calls, _ = scenario
    config = request.config.config.to_dict()
    config["execution"]["require_fresh_process"] = fresh
    request = replace(
        request, config=replace(request.config, config=Document.parse("config", config))
    )
    code, original = asyncio.run(execute_async(request, deps))
    assert code == 0
    root = Path(original.evidence_dir)
    original_bytes = {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}
    if dirty:
        state = read_json(locking.STATE_PATH)
        state["dirty"] = True
        from inferyard.contracts.validation import json_bytes

        locking.STATE_PATH.write_bytes(json_bytes(state))
    endpoint = config["endpoint"]
    rerun = CommandRequest(
        "run", from_run=root, server_pid=endpoint["server_pid"], endpoint_url=endpoint["url"]
    )
    with pytest.raises(PreflightError, match="rerun_requires_new_service"):
        asyncio.run(execute_async(rerun, deps))
    assert len(calls) == 8
    assert original_bytes == {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}
