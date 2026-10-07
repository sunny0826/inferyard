import asyncio
from dataclasses import replace

import pytest

from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import ContractError, Document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, sha256_file
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_trial_runner import inputs, scenario

__all__ = ["scenario"]


def prepare(scenario, policy):
    plan, loaded, deps, output = inputs(scenario)
    config = loaded.config.to_dict()
    config["conditions"]["cache_policy"] = policy
    loaded = replace(loaded, config=Document.parse("config", config))
    experiment = plan["experiment"]
    experiment["workloads"][0]["cache_protocol"] = "prism_prefix_reuse.v1"
    return compile_plan(experiment), loaded, deps, output


@pytest.mark.parametrize("policy", ["enabled", "disabled"])
def test_frozen_prefix_reuse_trial_and_offline_replay(scenario, policy):
    plan, loaded, deps, output = prepare(scenario, policy)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    calls = scenario[2]
    assert len(calls) == 8
    assert calls[0]["cache_prompt"] is False
    assert all(c["cache_prompt"] is (policy == "enabled") for c in calls[1:])
    assert read_trial(root)["summary"] == data["summary"]
    assert read_json(root / "effective-stream.json")["cache"]["value"] == policy
    assert data["summary"]["counts"]["completed"] == 3
    # Rehash the sealed request too: raw policy still must match the frozen protocol.
    path = sorted(root.glob("*.request.json"))[0]
    body = read_json(path)
    body["cache_prompt"] = not body["cache_prompt"]
    path.write_bytes(json_bytes(body))
    manifest = read_json(root / "manifest.json")
    manifest["files"][path.name].update(sha256=sha256_file(path), bytes=path.stat().st_size)
    (root / "manifest.json").write_bytes(json_bytes(manifest))
    with pytest.raises(EvidenceError, match="cache_protocol_request_policy_mismatch"):
        read_trial(root)


def test_enabled_without_protocol_remains_rejected(scenario):
    plan, loaded, deps, output = prepare(scenario, "enabled")
    exp = plan["experiment"]
    exp["workloads"][0].pop("cache_protocol")
    plan = compile_plan(exp)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code != 0 and not scenario[2]


def test_unknown_policy_is_rejected_before_any_request(scenario):
    plan, loaded, deps, output = prepare(scenario, "unknown")
    with pytest.raises(ContractError, match="cache_protocol"):
        asyncio.run(
            run_trial(
                plan,
                plan["trials"][0]["trial_id"],
                loaded,
                output,
                diagnostic=True,
                dependencies=deps,
            )
        )
    assert not scenario[2]


def test_prefix_reuse_and_strict_output_replay_together(scenario):
    from tests.integration.test_fixed_output_lifecycle import strict_inputs

    plan, loaded, deps, output = strict_inputs(scenario)
    config = loaded.config.to_dict()
    config["conditions"]["cache_policy"] = "enabled"
    loaded = replace(loaded, config=Document.parse("config", config))
    exp = plan["experiment"]
    exp["workloads"][0]["cache_protocol"] = "prism_prefix_reuse.v1"
    plan = compile_plan(exp)
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    assert scenario[2][0]["cache_prompt"] is False
    assert all(c["ignore_eos"] is True and c["n_cache_reuse"] == 0 for c in scenario[2])
    assert all(c["cache_prompt"] is True for c in scenario[2][1:])
    assert read_trial(root)["summary"] == data["summary"]


def test_prefix_reuse_refuses_unsealed_engine_evidence(scenario):
    plan, loaded, deps, output = prepare(scenario, "enabled")
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    manifest = read_json(root / "manifest.json")
    del manifest["files"]["service.props.json"]
    (root / "manifest.json").write_bytes(json_bytes(manifest))
    with pytest.raises(EvidenceError, match="cache_protocol_engine_unsealed"):
        read_trial(root)


def test_cache_sequence_rebuilt_from_all_raw_request_phases(scenario):
    scenario[3]["timings"] = dict(cache_n=0, prompt_n=1, prompt_ms=1, predicted_n=2, predicted_ms=1)
    plan, loaded, deps, output = prepare(scenario, "enabled")
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    cache = data["summary"]["cache_observations"]
    assert cache["observed_requests"] == cache["started_requests"] == 8
    assert cache["planned_formal_requests"] == 3
    assert cache["initial_no_reuse_observed"] is True
    assert cache["policy_consistent"] is True
    assert not any(r["reuse_observed"] for r in cache["rows"])
    assert [r["phase"] for r in cache["rows"]] == ["probe"] * 2 + ["warmup"] * 3 + ["formal"] * 3
    assert read_trial(root)["summary"]["cache_observations"] == cache


def test_two_frozen_cache_policies_compare_with_replayed_evidence(scenario, monkeypatch):
    from copy import deepcopy

    from inferyard.application.types import CommandRequest
    from inferyard.reporting.comparison_report import build_comparison, execute, verify_comparison
    from tests.unit.test_environment import state

    # Synthetic admission only; does not approve any real corpus.
    monkeypatch.setattr("inferyard.runtime.trial_runner.require_review", lambda bundle: None)
    snapshot = state()
    snapshot["platform"] = "synthetic-cache-protocol-platform"
    scenario[1].preflight = lambda config: (
        {
            "origin": "http://127.0.0.1:8080",
            "environment": deepcopy(snapshot),
            "verification": "verified",
        },
        [],
    )
    scenario[1].environment = lambda: deepcopy(snapshot)
    scenario[3]["timings"] = dict(cache_n=0, prompt_n=1, prompt_ms=1, predicted_n=2, predicted_ms=1)
    roots = []
    for policy in ("disabled", "enabled"):
        scenario[2].clear()
        plan, loaded, deps, output = prepare(scenario, policy)
        exp = plan["experiment"]
        exp["comparison"] = {"mode": "config", "factor": "cache_policy"}
        plan = compile_plan(exp)
        code, data, root = asyncio.run(
            run_trial(plan, plan["trials"][0]["trial_id"], loaded, output, dependencies=deps)
        )
        assert code == 0 and data["summary"]["completeness"] == "complete"
        roots.append(root)
    before = {str(p): sha256_file(p) for root in roots for p in root.iterdir() if p.is_file()}
    result = build_comparison(*roots)
    assert result["eligibility"]["quality"], result["blockers"]
    assert result["eligibility"]["completion"]
    assert result["eligibility"]["performance"] is False
    dest = roots[0].parent / "cache-comparison"
    code, _ = execute(CommandRequest("compare", left=roots[0], right=roots[1], out=dest))
    assert code == 0
    assert {"comparison.json", "index.json", "report.html", "artifact-manifest.json"} <= {
        path.name for path in dest.iterdir()
    }
    assert verify_comparison(dest)["verified"]
    assert before == {
        str(p): sha256_file(p) for root in roots for p in root.iterdir() if p.is_file()
    }


def test_cache_protocol_preserves_formal_failure_and_next_request(scenario):
    scenario[3]["fail_index"] = 6
    plan, loaded, deps, output = prepare(scenario, "enabled")
    code, data, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 0
    assert [r["execution_state"] for r in data["requests"]] == ["completed", "failed", "completed"]
    assert read_trial(root)["summary"] == data["summary"]
