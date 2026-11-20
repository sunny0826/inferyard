"""Portable raw receipt bytes and independently checked conversion chains."""

import hashlib
import json
from copy import deepcopy

import pytest

from inferyard.contracts.lineage_records import validate_records
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.contracts.plan_math import plan_hash
from inferyard.config.planning import prepare_plan, write_plan
from inferyard.errors import ContractError
from inferyard.analysis.ledger import read_trial
from tests.integration.test_phase2_plan import input_package
from tests.legacy_journal import LegacyJournal
from tests.unit.model_lineage_helpers import lineage


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
    store = LegacyJournal(
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
