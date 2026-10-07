"""Synthetic live regressions for A; no real model or platform qualification."""

import asyncio
import json
from copy import deepcopy
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.analysis import scoring
from inferyard.cli import main
from inferyard.contracts.validation import Document
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json
from inferyard.evidence.token_budgets import V1, V2
from inferyard.platforms.identity import PreflightError
from inferyard.reporting.rescore import verify_rescore, write_rescore
from inferyard.runtime.runner import execute_async
from inferyard.runtime.trial_runner import run_trial
from tests.helpers import fixture_run
from tests.integration.test_lab_cli import lab_scenario as lab_fixture
from tests.integration.test_runner import scenario as runner_fixture
from tests.integration.test_trial_runner import inputs

scenario = runner_fixture
lab_scenario = lab_fixture


@pytest.mark.parametrize("kind,warmups", [("check", 0), ("check", 3), ("run", 0), ("run", 3)])
def test_budget_only_inputs_that_will_be_sent(scenario, monkeypatch, kind, warmups):
    request, deps, calls, _ = scenario
    config = request.config.config.to_dict()
    config["execution"]["warmup_count"] = warmups
    request = replace(
        request,
        command=kind,
        config=replace(request.config, config=Document.parse("config", config)),
    )
    budget_calls, hashes = [], []
    adapter_factory, original_hash = deps.adapter, scoring.scorer_hash

    def counted_hash():
        hashes.append(1)
        return original_hash()

    monkeypatch.setattr(scoring, "scorer_hash", counted_hash)

    def factory(*args, **kwargs):
        adapter = adapter_factory(*args, **kwargs)
        original = adapter.token_budget

        async def budget(cfg, prompt):
            budget_calls.append(prompt)
            if prompt == cfg["execution"]["warmup_prompt"] and (kind != "run" or warmups == 0):
                raise PreflightError("unused_input_must_not_block")
            return await original(cfg, prompt)

        adapter.token_budget = budget
        return adapter

    deps.adapter = factory
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    assert hashes == [1]
    expected = [config["execution"]["probe_prompt"]]
    if kind == "run":
        if warmups:
            expected.append(config["execution"]["warmup_prompt"])
        expected += [c["prompt"] for c in request.config.bundle.to_dict()["cases"]]
    assert budget_calls == expected
    root = Path(result.evidence_dir)
    assert not (root / V1).exists()
    snapshot = read_json(root / V2)
    assert len(snapshot["entries"]) == len(expected)
    assert len(calls) == (2 if kind == "check" else 2 + warmups + len(expected) - 1 - bool(warmups))
    assert read_trial(root)["summary"]["counts"]["planned"] == len(
        request.config.bundle.to_dict()["cases"]
    )


def test_trial_and_resume_budget_follow_selected_cases(scenario):
    plan, loaded, deps, output = inputs(scenario)
    tid = plan["trials"][0]["trial_id"]
    # Interrupt after one formal attempt; the resume must budget only unexecuted cases.
    scenario[3]["fail_index"] = 5
    deps.guard = lambda *_: None
    original_scorer = deps.scorer

    def stop_after_first(*args):
        raise EvidenceError("synthetic storage boundary failure")

    scenario[3]["fail_index"] = None
    deps.scorer = stop_after_first
    code, parent, _ = asyncio.run(
        run_trial(plan, tid, loaded, output, dependencies=deps, diagnostic=True)
    )
    assert code == 4
    selected = [r["case_id"] for r in parent["requests"] if r["execution_state"] == "not_executed"]
    deps.scorer = original_scorer
    code, _, root = asyncio.run(
        run_trial(
            plan,
            tid,
            loaded,
            output,
            dependencies=deps,
            diagnostic=True,
            parent=parent,
            resume_case_ids=selected,
        )
    )
    assert code == 0
    entries = read_json(root / V2)["entries"]
    assert [e["case_id"] for e in entries if e["phase"] == "formal"] == selected


@pytest.mark.parametrize(
    "damage", ["extra", "boolean", "duplicate", "wrong-case", "conflict", "order", "formal-null"]
)
def test_lab_budget_v2_rejects_bad_shape_and_inventory(lab_scenario, damage):
    command, deps, _ = lab_scenario
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    (root / "manifest.json").unlink()
    value = read_json(root / V2)
    if damage == "order":
        value["entries"].reverse()
    elif damage == "formal-null":
        next(e for e in value["entries"] if e["phase"] == "formal")["case_id"] = None
    elif damage == "extra":
        value["entries"][0]["budget"]["extra"] = 1
    elif damage == "boolean":
        value["entries"][0]["budget"]["input_tokens"] = True
    elif damage == "duplicate":
        value["entries"].append(deepcopy(value["entries"][0]))
    elif damage == "wrong-case":
        value["entries"][-1]["case_id"] = "not-selected"
    else:
        (root / V1).write_bytes(json_bytes([]))
    (root / V2).write_bytes(json_bytes(value))
    before = {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}
    expected = UnsupportedFormat if damage == "conflict" else EvidenceError
    with pytest.raises(expected):
        read_trial(root)
    assert before == {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}


def test_lab_budget_v1_is_unsupported_without_rewriting(lab_scenario):
    command, deps, _ = lab_scenario
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    (root / "manifest.json").unlink()
    entries = read_json(root / V2)["entries"]
    (root / V2).unlink()
    (root / V1).write_bytes(json_bytes([e["budget"] for e in entries]))
    before = {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}
    with pytest.raises(UnsupportedFormat, match="unsupported_format") as error:
        read_trial(root)
    assert error.value.saved == V1
    assert before == {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}


@pytest.mark.parametrize(
    "error,reason,code",
    [
        (PreflightError("rerun_tool_or_scorer_changed"), "rerun_tool_or_scorer_changed", 2),
        (EvidenceError("rescore_scorer_identity_changed"), "rescore_scorer_identity_changed", 4),
        (EvidenceError("original_evidence_hash_mismatch"), "original_evidence_hash_mismatch", 4),
        (PreflightError("secret_credential_value"), "preflight_blocked", 2),
        (EvidenceError("secret_credential_value"), "evidence_error", 4),
    ],
)
def test_safe_entrypoint_reasons(error, reason, code, capsys):
    def handler(_):
        raise error

    assert main(["rescore-check", "--run", "/unused"], handlers={"rescore-check": handler}) == code
    captured = capsys.readouterr()
    assert json.loads(captured.out)["limitations"] == [reason]
    assert "secret_credential_value" not in captured.out + captured.err


def test_changed_scorer_cli_and_lineage_hash_check(tmp_path, monkeypatch, capsys):
    root = fixture_run(tmp_path / "source")
    first = tmp_path / "first"
    second = tmp_path / "second"
    write_rescore(root, first, scorer_id="phase2.v1", reason="first")
    write_rescore(
        root, second, scorer_id="phase2.v2", reason="second", parent_path=first / "analysis.json"
    )
    monkeypatch.setattr(scoring, "scorer_hash", lambda: "f" * 64)
    assert main(["rescore-check", "--run", str(second)]) == 4
    assert json.loads(capsys.readouterr().out)["limitations"] == ["rescore_scorer_identity_changed"]
    value = read_json(second / "parent-lineage.json")
    value["rescore.json"]["reason"] += " tampered"
    (second / "parent-lineage.json").write_bytes(json_bytes(value))
    with pytest.raises(EvidenceError, match="rescore_parent_recomputation_mismatch"):
        verify_rescore(second)


def test_runtime_evidence_reason_survives(scenario):
    request, deps, _, _ = scenario

    def guard(*_):
        raise EvidenceError("formal_request_out_of_order")

    deps.guard = guard
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 4
    assert "formal_request_out_of_order" in result.limitations


def test_batch_reuses_source_and_score_identities_across_trials(tmp_path, scenario, monkeypatch):
    from inferyard.runtime import batch_runner, batch_state
    from tests.integration.test_batch_runner import batch

    request, deps = batch(tmp_path, scenario)
    calls = {"score": 0, "source": 0}
    old_score, old_source = scoring.scorer_hash, batch_runner.tool_source_hash

    def score_hash():
        calls["score"] += 1
        return old_score()

    def source_hash(**kwargs):
        calls["source"] += 1
        return old_source(**kwargs)

    monkeypatch.setattr(scoring, "scorer_hash", score_hash)
    monkeypatch.setattr(batch_runner, "tool_source_hash", source_hash)
    monkeypatch.setattr(batch_state, "tool_source_hash", source_hash)
    code, result = asyncio.run(batch_runner.execute_async(request, deps))
    assert code == 0 and len(result.details["runs"]) == 3
    assert calls == {"score": 1, "source": 1}
    calls.update(score=0, source=0)
    code, _ = asyncio.run(batch_runner.execute_async(request, deps))
    assert code == 0 and calls == {"score": 0, "source": 1}
