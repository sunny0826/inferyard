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


@pytest.mark.parametrize("damage", ["identity", "category", "storage"])
def test_identity_and_storage_errors_stop_independently(scenario, damage):
    request, deps, calls, _ = scenario

    def score(case, answer, policy):
        if damage == "storage":
            raise EvidenceError("evidence_io_error")
        result = score_case(case, answer, policy)
        result["scorer_sha256" if damage == "identity" else "category"] = (
            "0" * 64 if damage == "identity" else "math"
        )
        return result

    deps.scorer = score
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 4
    expected = "evidence_io_error" if damage == "storage" else "scorer_identity_mismatch"
    assert expected in result.limitations
    data = read_trial(Path(result.evidence_dir))
    assert data["summary"]["counts"]["not_executed"] == 2
    assert len(calls) == 6


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


def test_preflight_diagnostic_flag_is_forwarded(scenario, monkeypatch):
    from inferyard.contracts.environment_admission import admission
    from inferyard.platforms import identity

    request, deps, _, _ = scenario
    old = deps.preflight

    def preflight(config, *, diagnostic=False, environment_policy=None):
        observed, files = old(config)
        env = {**observed["environment"], "ac_online": None}
        result = admission(
            config["conditions"], env, diagnostic=diagnostic, policy=environment_policy
        )
        if result["blockers"]:
            raise PreflightError("frozen_environment_mismatch")
        return {**observed, "environment": env, "environment_admission": result}, files

    monkeypatch.setattr(identity, "static_preflight", preflight)
    deps.preflight = preflight
    for command, diagnostic, expected in [("run", False, 2), ("run", True, 3), ("check", False, 0)]:
        code, result = asyncio.run(
            execute_async(replace(request, command=command, diagnostic=diagnostic), deps)
        )
        assert code == expected
        if expected != 2:
            observed = TrialEvidence(Path(result.evidence_dir)).observation("identity")
            assert "ac_online" in observed["environment_admission"]["differences"]


def test_batch_scoring_failures_do_not_drop_later_trials(tmp_path, scenario):
    from inferyard.runtime import batch_runner
    from tests.integration.test_batch_runner import batch

    request, deps = batch(tmp_path, scenario)

    def broken(*_):
        raise RuntimeError("isolated scoring failure")

    deps.scorer = broken
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 3
    assert len(result.details["runs"]) == 3
    assert result.completeness == "incomplete"
    for path in result.details["runs"]:
        data = read_trial(Path(path))
        assert data["summary"]["counts"]["not_executed"] == 0
        assert data["summary"]["counts"]["valid_executed"] == 3


def test_applicable_handoff_reuses_clean_service(tmp_path, scenario, monkeypatch):
    from inferyard.runtime import batch_runner, batch_state
    from tests.integration.test_batch_runner import batch

    request, deps = batch(tmp_path, scenario, second=True)
    code, first = asyncio.run(execute_async(request, deps))
    assert code == 3 and first.status == "awaiting_handoff"
    endpoint = scenario[0].config.config.to_dict()["endpoint"]
    monkeypatch.setattr(
        batch_state, "process_start_ticks", lambda _: endpoint["process_start_ticks"]
    )
    handoff = replace(
        request,
        workload_id="w2",
        endpoint_url=endpoint["url"],
        server_pid=endpoint["server_pid"],
        handoff_note="same clean service",
    )
    code, result = asyncio.run(execute_async(handoff, deps))
    assert code == 0
    assert len(scenario[2]) == 16
    binding = TrialEvidence(Path(result.details["runs"][-1])).observation("service_binding")
    assert binding["transition"] == "same_process"
    assert binding["cache_state"] == "unknown"


@pytest.mark.parametrize("check_environment", [False, True])
def test_diagnostic_trial_respects_explicit_environment_safety(scenario, check_environment):
    from inferyard.runtime.safety import SafetyGuard
    from inferyard.runtime.trial_runner import run_trial
    from tests.unit.test_safety import Sensors

    plan, loaded, deps, output = inputs(scenario)
    plan["experiment"]["environment_admission"] = {
        "definition": "environment-admission.v2",
        "required_fields": [],
    }
    plan["experiment"]["safety"] = dict(
        interval_seconds=1,
        check_environment=check_environment,
        max_temperature_celsius=90,
        require_temperature=False,
        max_external_cpu_percent=None,
    )
    from inferyard.config.planning import compile_plan

    plan = compile_plan(plan["experiment"])
    env = deps.environment()
    deps.environment = lambda: {**env, "ac_online": None}
    deps.safety = lambda policy, config, environment: SafetyGuard(
        policy, config, environment, sensors=Sensors(40)
    )
    code, data, _ = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == (2 if check_environment else 0)
    if check_environment:
        assert data["summary"]["stop_reason"] == "environment_safety_condition_changed"
        assert not scenario[2]
    else:
        assert data["summary"]["counts"]["completed"] == 3
