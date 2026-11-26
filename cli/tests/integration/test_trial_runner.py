"""Full preflight/probe/warmup lifecycle, with a real parser and synthetic service."""

import asyncio
import sys
from pathlib import Path

import pytest

from inferyard.analysis.scoring import score_case
from inferyard.config.planning import compile_plan
from inferyard.evidence.storage import read_json, sha256_file
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario as runner_scenario
from tests.unit.phase2_contract_helpers import experiment

scenario = runner_scenario


@pytest.mark.parametrize(
    "timings",
    [
        {"cache_n": 0, "prompt_n": 1, "prompt_ms": 1, "predicted_n": 2, "predicted_ms": 2},
        {"sensitive": "fixture-secret-not-to-persist"},
    ],
)
def test_engine_timings_rebuild_offline_with_bound_build_and_unchanged_evidence(scenario, timings):
    from inferyard.analysis.ledger import read_trial

    scenario[3]["timings"] = timings
    plan, loaded, deps, output = inputs(scenario)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    assert all(r["engine_build_verified"] for r in data["requests"])
    before = {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
    rebuilt = read_trial(root)
    assert before == {p.name: sha256_file(p) for p in root.iterdir() if p.is_file()}
    metrics = [
        m
        for m in rebuilt["summary"]["metric_observations"]
        if m["metric_id"] == "L07" and m["request_id"]
    ]
    assert len(metrics) == 3
    for item in metrics:
        assert not item["comparison_eligible"]
        assert item["value"] == (500 if "predicted_n" in timings else None)
        assert item["missing_reason"] == (
            None if "predicted_n" in timings else "invalid_engine_timings"
        )
        assert item["sampled_start_ns"] is None
        for ref in item["evidence_refs"]:
            verify_reference(root, ref)
    assert TrialEvidence(root).has_observation("service_properties")
    assert "fixture-secret-not-to-persist" not in (root / "events.jsonl").read_text()


def inputs(scenario):
    request, deps, calls, settings = scenario
    deps.scorer = score_case
    config = request.config.config.to_dict()
    source = experiment()
    source["budget"]["max_wall_seconds"] = 100_000
    source["workloads"][0]["timeout_seconds"] = config["execution"]["timeout_seconds"]
    source["workloads"][0]["protocol"]["case_ids"] = [
        c["case_id"] for c in request.config.bundle.to_dict()["cases"]
    ]
    return compile_plan(source), request.config, deps, Path(config["output"]["root"])


def test_memory_safety_stop_before_generation(scenario, monkeypatch):
    import inferyard.runtime.trial_runner as runner

    plan, loaded, deps, output = inputs(scenario)
    monkeypatch.setattr(runner, "memory_available", lambda: 0)
    code, data, _ = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 2 and not scenario[2]
    assert data["summary"]["stop_reason"] == "memory_safety_budget_reached"


def test_budget_cancellation_is_not_mislabeled_as_user_cancellation(scenario):
    plan, loaded, deps, output = inputs(scenario)
    factory = deps.adapter

    def adapter(*args, **kwargs):
        instance = factory(*args, **kwargs)
        original = instance.generate

        async def generate(*args, **kwargs):
            if len(scenario[2]) == 0:
                await asyncio.sleep(2)
            return await original(*args, **kwargs)

        instance.generate = generate
        return instance

    deps.adapter = adapter
    code, data, _ = asyncio.run(
        run_trial(
            plan,
            plan["trials"][0]["trial_id"],
            loaded,
            output,
            diagnostic=True,
            dependencies=deps,
            wall_budget_seconds=0.7,
        )
    )
    assert code == 3
    assert data["summary"]["stop_reason"] == "trial_wall_budget_exhausted"
    assert data["requests"][0]["error_category"] == "trial_wall_budget_exhausted_before_send"
    assert data["summary"]["counts"]["cancelled"] == 1
