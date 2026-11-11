"""Warmup is a frozen workload choice; defaults and source binding remain intact."""

import hashlib
from copy import deepcopy

import pytest
from jsonschema import Draft202012Validator

from inferyard.config.loader import load_config
from inferyard.config.single_plan import compile_single_plan
from inferyard.contracts.schemas import export_schema
from inferyard.contracts.validation import Document
from inferyard.errors import ContractError
from inferyard.evidence.storage import json_bytes


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
    assert old["plan_sha256"] != new["plan_sha256"]
    assert old["total_budget_seconds"] > new["total_budget_seconds"]
    assert old["request_limit"] == new["request_limit"] == len(loaded.bundle.to_dict()["cases"])
