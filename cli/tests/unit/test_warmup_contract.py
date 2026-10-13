"""Warmup is a frozen workload choice; defaults and source binding remain intact."""

import hashlib
from copy import deepcopy

import pytest
from jsonschema import Draft202012Validator

from inferyard.config.loader import load_config
from inferyard.config.plan_math import plan_hash
from inferyard.config.single_plan import compile_single_plan
from inferyard.contracts.schemas import export_schema
from inferyard.contracts.validation import ContractError, Document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import json_bytes
from tests.unit.test_ledger import attempt, finish, journal, setup  # noqa: F401


@pytest.mark.parametrize("kind", ["config_input", "config"])
@pytest.mark.parametrize("count", [0, 1, 2, 3])
def test_warmup_range_accepted_by_runtime_and_exported_schema(config_path, kind, count):
    config = load_config(config_path).config.to_dict()
    config["execution"]["warmup_count"] = count
    Draft202012Validator(export_schema(kind)).validate(config)
    assert Document.parse(kind, config).to_dict()["execution"]["warmup_count"] == count


@pytest.mark.parametrize("kind", ["config_input", "config"])
@pytest.mark.parametrize("count", [-1, 4, True, "0"])
def test_warmup_rejects_bounds_and_wrong_types(config_path, kind, count):
    config = load_config(config_path).config.to_dict()
    config["execution"]["warmup_count"] = count
    assert not Draft202012Validator(export_schema(kind)).is_valid(config)
    with pytest.raises(ContractError) as error:
        Document.parse(kind, config)
    assert error.value.path == f"{kind}.execution.warmup_count"


def test_warmup_default_stays_three_and_choice_changes_plan_binding(config_path):
    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    assert config["execution"]["warmup_count"] == 3
    old = compile_single_plan(config, loaded.bundle.to_dict(), experiment_id="warmup-binding")
    changed = deepcopy(config)
    changed["execution"]["warmup_count"] = 0
    new = compile_single_plan(changed, loaded.bundle.to_dict(), experiment_id="warmup-binding")
    for plan, frozen in ((old, config), (new, changed)):
        binding = plan["experiment"]["workloads"][0]["config"]
        assert binding["sha256"] == hashlib.sha256(json_bytes(frozen)).hexdigest()
        assert plan["plan_sha256"] == plan_hash(plan)
    assert old["plan_sha256"] != new["plan_sha256"]
    assert old["total_budget_seconds"] > new["total_budget_seconds"]
    assert old["request_limit"] == new["request_limit"] == len(loaded.bundle.to_dict()["cases"])


def test_legacy_three_warmup_plan_and_run_read_without_rewriting(tmp_path, setup):  # noqa: F811
    # Existing v3 fixture shape; no new field or migration is needed by this extension.
    config, _, plan = setup
    assert config["execution"]["warmup_count"] == 3
    store = journal(tmp_path, setup)
    attempt(store, setup, 0, "completed")
    original = finish(store)
    before = {p.name: p.read_bytes() for p in store.path.iterdir() if p.is_file()}
    assert Document.parse("plan", plan).to_dict() == plan
    reread = read_trial(store.path)
    assert reread["config"]["execution"]["warmup_count"] == 3
    assert reread["run"] == original["run"] and reread["summary"] == original["summary"]
    assert before == {p.name: p.read_bytes() for p in store.path.iterdir() if p.is_file()}
