from copy import deepcopy

import pytest

from inferyard.config.model_lineage import validate_lineage
from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import ContractError
from tests.unit.test_phase2_contracts import experiment


def lineage():
    return {
        "definition": "declared_quantization_lineage.v1",
        "base_repo": "example/base",
        "base_revision": "a" * 40,
        "base_artifacts": [{"name": "weights", "sha256": "b" * 64}],
        "tokenizer_sha256": "c" * 64,
        "conversion_tool_sha256": "d" * 64,
        "conversion_recipe_sha256": "e" * 64,
        "quantization_tool_sha256": "f" * 64,
        "quantization_recipe_sha256": "1" * 64,
        "output_sha256": "2" * 64,
        "packing": "Q4_K_M",
    }


def test_lineage_is_frozen_without_modifying_existing_plan_shape():
    exp = experiment()
    original = compile_plan(exp)
    exp["workloads"][0]["model_lineage"] = lineage()
    first = compile_plan(exp)
    assert first["plan_sha256"] != original["plan_sha256"]
    exp["workloads"][0]["model_lineage"]["base_revision"] = "3" * 40
    assert compile_plan(exp)["plan_sha256"] != first["plan_sha256"]
    assert "model_lineage" not in original["experiment"]["workloads"][0]


@pytest.mark.parametrize(
    "revision",
    ["main", "latest", "v1", "a" * 39, "g" * 40, "a" * 65, *["a" * n for n in range(41, 64)]],
)
def test_mutable_or_invalid_revision_rejected(revision):
    exp = experiment()
    exp["workloads"][0]["model_lineage"] = {**lineage(), "base_revision": revision}
    with pytest.raises(ContractError):
        compile_plan(exp)


@pytest.mark.parametrize("length", [40, 64])
def test_complete_revision_identifiers_are_preserved(length):
    exp = experiment()
    exp["workloads"][0]["model_lineage"] = {**lineage(), "base_revision": "a" * length}
    plan = compile_plan(exp)
    assert plan["experiment"]["workloads"][0]["model_lineage"]["base_revision"] == "a" * length


@pytest.mark.parametrize("key", list(lineage()))
def test_incomplete_lineage_rejected(key):
    exp = experiment()
    declaration = lineage()
    declaration.pop(key)
    exp["workloads"][0]["model_lineage"] = declaration
    with pytest.raises(ContractError):
        compile_plan(exp)


def test_output_binding_and_duplicate_artifacts():
    declared = lineage()
    config = {"model": {"sha256": declared["output_sha256"], "packing": declared["packing"]}}
    workload = {"model_lineage": declared}
    validate_lineage(workload, config)
    validate_lineage({}, config)
    for key in ("sha256", "packing"):
        changed = deepcopy(config)
        changed["model"][key] = "other"
        with pytest.raises(ContractError, match="output differs"):
            validate_lineage(workload, changed)
    declared["base_artifacts"].append(deepcopy(declared["base_artifacts"][0]))
    with pytest.raises(ContractError, match="duplicate"):
        validate_lineage(workload, config)
