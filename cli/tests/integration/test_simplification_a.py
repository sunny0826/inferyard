"""Synthetic input admission and offline evidence regressions; no native qualification."""

import asyncio
import json
from copy import deepcopy
from dataclasses import replace
from pathlib import Path

import pytest

from inferyard.analysis import scoring
from inferyard.cli import main
from inferyard.contracts.validation import Document, json_bytes
from inferyard.errors import EvidenceError, PreflightError, UnsupportedFormat
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import read_json
from inferyard.evidence.token_budgets import V1
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.reporting.rescore import verify_rescore, write_rescore
from inferyard.runtime.runner import execute_async
from tests.helpers import fixture_run
from tests.integration.test_lab_cli import lab_scenario as lab_fixture
from tests.integration.test_runner import scenario as runner_fixture
from tests.layout_mutations import change_observation

scenario = runner_fixture
lab_scenario = lab_fixture


@pytest.mark.parametrize("kind,warmups", [("check", 0), ("check", 3), ("run", 0), ("run", 3)])
def test_budget_only_inputs_that_will_be_sent(scenario, kind, warmups):
    request, deps, calls, _ = scenario
    config = request.config.config.to_dict()
    config["execution"]["warmup_count"] = warmups
    request = replace(
        request,
        command=kind,
        config=replace(request.config, config=Document.parse("config", config)),
    )
    adapter_factory = deps.adapter

    def factory(*args, **kwargs):
        adapter = adapter_factory(*args, **kwargs)
        original = adapter.token_budget

        async def budget(cfg, prompt):
            if prompt == cfg["execution"]["warmup_prompt"] and (kind != "run" or warmups == 0):
                raise PreflightError("unused_input_must_not_block")
            return await original(cfg, prompt)

        adapter.token_budget = budget
        return adapter

    deps.adapter = factory
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0
    cases = len(request.config.bundle.to_dict()["cases"])
    assert len(calls) == (2 if kind == "check" else 2 + warmups + cases)
    assert read_trial(Path(result.evidence_dir))["summary"]["counts"]["planned"] == cases


@pytest.mark.parametrize(
    "damage", ["extra", "boolean", "duplicate", "wrong-case", "conflict", "order", "formal-null"]
)
def test_lab_budget_v2_rejects_bad_shape_and_inventory(lab_scenario, damage):
    command, deps, _ = lab_scenario
    code, result = asyncio.run(execute_async(command, deps))
    assert code == 0
    root = Path(result.evidence_dir)
    (root / "manifest.json").unlink()
    value = TrialEvidence(root).observation("token_budgets")
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
    change_observation(root, "token_budgets", lambda payload: payload.update(value))
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
    entries = TrialEvidence(root).observation("token_budgets")["entries"]
    change_observation(root, "token_budgets", remove=True)
    (root / V1).write_bytes(json_bytes([e["budget"] for e in entries]))
    before = {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}
    with pytest.raises(UnsupportedFormat, match="unsupported_format") as error:
        read_trial(root)
    assert error.value.saved == V1
    assert before == {p.name: p.read_bytes() for p in root.iterdir() if p.is_file()}


def test_changed_scorer_cli_and_lineage_hash_check(tmp_path, monkeypatch, capsys):
    root = fixture_run(tmp_path / "source")
    first = tmp_path / "first"
    second = tmp_path / "second"
    write_rescore(root, first, scorer_id="phase2.v1", reason="first")
    write_rescore(
        root, second, scorer_id="phase2.v2", reason="second", parent_path=first / "analysis.json"
    )
    monkeypatch.setattr(scoring, "scorer_hash", lambda: "f" * 64)
    assert main(["verify", "--path", str(second)]) == 4
    assert json.loads(capsys.readouterr().out)["limitations"] == ["rescore_scorer_identity_changed"]
    value = read_json(second / "parent-lineage.json")
    value["rescore.json"]["reason"] += " tampered"
    (second / "parent-lineage.json").write_bytes(json_bytes(value))
    with pytest.raises(EvidenceError, match="rescore_parent_recomputation_mismatch"):
        verify_rescore(second)
