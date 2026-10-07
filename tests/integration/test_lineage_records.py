"""Portable raw receipt bytes and independently checked conversion chains."""

import hashlib
import json
from copy import deepcopy

import pytest

from inferyard.config.lineage_records import validate_records
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.config.plan_math import plan_hash
from inferyard.config.planning import prepare_plan, write_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import read_trial
from tests.integration.test_phase2_plan import input_package
from tests.unit.test_model_lineage import lineage


def encoded(record):
    text = json.dumps(record, indent=2) + "\n"
    return {"text": text, "sha256": hashlib.sha256(text.encode()).hexdigest()}


def records(declaration):
    conversion = {
        k: deepcopy(declaration[k])
        for k in ("base_repo", "base_revision", "base_artifacts", "tokenizer_sha256")
    }
    conversion.update(
        definition="conversion_receipt.v1",
        tool_sha256=declaration["conversion_tool_sha256"],
        recipe_sha256=declaration["conversion_recipe_sha256"],
        output_sha256="9" * 64,
        exit_code=0,
    )
    quantization = dict(
        definition="quantization_receipt.v1",
        input_sha256=conversion["output_sha256"],
        tool_sha256=declaration["quantization_tool_sha256"],
        recipe_sha256=declaration["quantization_recipe_sha256"],
        output_sha256=declaration["output_sha256"],
        packing=declaration["packing"],
        exit_code=0,
    )
    return {"conversion": encoded(conversion), "quantization": encoded(quantization)}


def package(tmp_path, config_path):
    path = input_package(tmp_path, config_path)
    exp = json.loads(path.read_text())
    config = json.loads((path.parent / "config.json").read_text())
    declaration = lineage()
    declaration.update(output_sha256=config["model"]["sha256"], packing=config["model"]["packing"])
    exp["workloads"][0].update(
        model_lineage=declaration, model_lineage_records=records(declaration)
    )
    path.write_text(json.dumps(exp))
    return path


def test_receipts_survive_relocation_and_trial_offline_replay(tmp_path, config_path):
    path = package(tmp_path, config_path)
    frozen = tmp_path / "frozen"
    expected = write_plan(path, frozen)
    path.parent.rename(tmp_path / "removed")
    plan, configs = read_frozen_plan(frozen / "plan.json")
    assert plan == expected
    loaded = configs["w1"]
    store = TrialJournal(
        tmp_path / "runs",
        plan,
        plan["trials"][0]["trial_id"],
        loaded.config.to_dict(),
        loaded.bundle.to_dict(),
    )
    store.close()
    replay = read_trial(store.path)
    assert (
        replay["plan"]["experiment"]["workloads"][0]["model_lineage_records"]
        == (expected["experiment"]["workloads"][0]["model_lineage_records"])
    )
    # Rehash the containing plan/run too: internal chain checks still reject it.
    record = plan["experiment"]["workloads"][0]["model_lineage_records"]["quantization"]
    payload = json.loads(record["text"])
    payload["input_sha256"] = "8" * 64
    record.update(encoded(payload))
    plan["plan_sha256"] = plan_hash(plan)
    (store.path / "plan.json").write_text(json.dumps(plan))
    run_path = store.path / "run.json"
    run = json.loads(run_path.read_text())
    run["plan_sha256"] = plan["plan_sha256"]
    run_path.write_text(json.dumps(run))
    with pytest.raises(ContractError, match="chain broken"):
        read_trial(store.path)


@pytest.mark.parametrize(
    "stage,field",
    [
        ("conversion", "base_repo"),
        ("conversion", "base_revision"),
        ("conversion", "tokenizer_sha256"),
        ("conversion", "tool_sha256"),
        ("conversion", "recipe_sha256"),
        ("quantization", "tool_sha256"),
        ("quantization", "recipe_sha256"),
        ("quantization", "input_sha256"),
        ("quantization", "output_sha256"),
        ("quantization", "packing"),
    ],
)
def test_rehashed_inconsistent_receipts_rejected_before_freeze(tmp_path, config_path, stage, field):
    path = package(tmp_path, config_path)
    exp = json.loads(path.read_text())
    raw = exp["workloads"][0]["model_lineage_records"][stage]
    receipt = json.loads(raw["text"])
    receipt[field] = "8" * 64
    raw.update(encoded(receipt))
    path.write_text(json.dumps(exp))
    with pytest.raises(ContractError):
        prepare_plan(path)


@pytest.mark.parametrize(
    "corrupt", ["bytes", "json", "duplicate_key", "failed", "artifacts", "no_lineage"]
)
def test_raw_receipt_failures(corrupt):
    declaration = lineage()
    workload = {"model_lineage": declaration, "model_lineage_records": records(declaration)}
    raw = workload["model_lineage_records"]["conversion"]
    receipt = json.loads(raw["text"])
    if corrupt == "bytes":
        raw["text"] += " "
    elif corrupt in ("json", "duplicate_key"):
        raw["text"] = "{" if corrupt == "json" else '{"x":1,"x":2}'
        raw["sha256"] = hashlib.sha256(raw["text"].encode()).hexdigest()
    elif corrupt == "failed":
        receipt["exit_code"] = 1
        raw.update(encoded(receipt))
    elif corrupt == "artifacts":
        receipt["base_artifacts"].append(deepcopy(receipt["base_artifacts"][0]))
        raw.update(encoded(receipt))
    else:
        workload.pop("model_lineage")
    with pytest.raises(ContractError):
        validate_records(workload)


def test_frozen_reader_rechecks_receipt_chain_after_rehash(tmp_path, config_path):
    path = package(tmp_path, config_path)
    frozen = tmp_path / "frozen"
    plan = write_plan(path, frozen)
    raw = plan["experiment"]["workloads"][0]["model_lineage_records"]["conversion"]
    receipt = json.loads(raw["text"])
    receipt["output_sha256"] = "7" * 64
    raw.update(encoded(receipt))
    plan["plan_sha256"] = plan_hash(plan)
    (frozen / "plan.json").write_text(json.dumps(plan))
    with pytest.raises(ContractError, match="chain broken"):
        read_frozen_plan(frozen / "plan.json")


@pytest.mark.parametrize("length", [41, 63])
def test_rehashed_matching_but_incomplete_revision_rejected(tmp_path, config_path, length):
    path = package(tmp_path, config_path)
    frozen = tmp_path / "frozen"
    plan = write_plan(path, frozen)
    workload = plan["experiment"]["workloads"][0]
    workload["model_lineage"]["base_revision"] = "a" * length
    workload["model_lineage_records"] = records(workload["model_lineage"])
    # Matching declaration/receipt bytes and a new outer hash are insufficient:
    # the pinned revision must itself be a complete object identifier.
    plan["plan_sha256"] = plan_hash(plan)
    (frozen / "plan.json").write_text(json.dumps(plan))
    with pytest.raises(ContractError, match="base_revision"):
        read_frozen_plan(frozen / "plan.json")
    with pytest.raises(ContractError, match="base_revision"):
        validate_records(workload)
