"""Shared context speed wire contract regressions."""

import json
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis.engine_timing import engine_rates
from inferyard.contracts.context_speed import validate_response, validate_result_bindings
from inferyard.contracts.schemas import schemas_for
from inferyard.contracts.validation import ContractError, strict_json_loads, validate_document


def test_result_kind_registered():
    assert "context_speed_result" in schemas_for()


GOLDEN = (
    Path(__file__).resolve().parents[3]
    / "studio/internal/inferyard/testdata/context_speed_contract.json"
)
CASES = strict_json_loads(GOLDEN.read_text())


@pytest.mark.parametrize("case", CASES, ids=lambda c: c["name"])
def test_shared_golden(case):
    if case["valid"]:
        validate_response(case["response"], case["exit_code"])
    else:
        with pytest.raises(ContractError):
            validate_response(case["response"], case["exit_code"])


def test_independent_observable_expectations():
    cases = {c["name"]: c for c in CASES}
    full = cases["five_complete"]["response"]["details"]
    assert full["summary"]["columns"]["time"] == {"median": 3, "min": 1, "max": 5}
    assert full["summary"]["columns"]["decode"] == {"median": 85, "min": 51, "max": 255}
    for name in ("completed_missing_timing", "cohort_unknown", "five_valid_token_mismatch"):
        case = cases[name]
        assert case["exit_code"] == 0
        assert case["response"]["details"]["summary"] is None
        assert case["response"]["details"]["execution"]["counts"]["completed"] == 5
    zero = cases["zero_runs"]["response"]["details"]
    assert len(zero["samples"]) == 5
    assert zero["execution"]["counts"]["not_executed"] == 5
    assert zero["execution"]["stop_reason"]["missing_reason"] == "unknown"
    cached = cases["cached_hand_calculation"]["response"]["details"]
    assert [cached["samples"][0]["columns"][k]["value"] for k in ("time", "prefill", "decode")] == [
        0.4,
        200,
        20,
    ]
    assert cached["summary"] is None
    assert not cached["samples"][0]["measurement"]["valid"]


@pytest.mark.parametrize("raw", ['{"a":1,"a":2}', '{"a":NaN}', '{"a":Infinity}'])
def test_raw_json_rejected(raw):
    with pytest.raises(ContractError):
        strict_json_loads(raw)


@pytest.mark.parametrize("bad", [float("nan"), float("inf"), float("-inf"), True, -1])
def test_nonfinite_boolean_negative_rejected(bad):
    data = deepcopy(CASES[0]["response"]["details"])
    data["samples"][0]["columns"]["time"]["value"] = bad
    with pytest.raises(ContractError):
        validate_document("context_speed_result", data)


def test_existing_timing_formula_hand_computed():
    raw = {
        "definition": "prism.adfffbe41.timings.v1",
        "final": True,
        "speculative": False,
        "missing_reason": None,
        "cache_n": 20,
        "prompt_n": 80,
        "predicted_n": 11,
        "prompt_ms": 400,
        "predicted_ms": 500,
    }
    rates = engine_rates(
        {"execution_state": "completed", "engine_build_verified": True, "engine_timings": [raw]}
    )
    assert rates["L06"]["value"] == 200
    assert rates["L07"]["value"] == 20
    assert raw["prompt_ms"] / 1000 == 0.4


def test_result_schema_is_lazy():
    from tests.unit.test_cli_architecture import (
        test_metadata_cold_start_does_not_import_execution_backends,
    )

    test_metadata_cold_start_does_not_import_execution_backends(
        ["--schema", "context_speed_result"]
    )


def test_bindings_reject_other_plan_trial_and_run():
    from inferyard.config.planning import compile_plan
    from tests.unit.test_context_speed_profile import speed_experiment

    plan = compile_plan(speed_experiment())
    data = deepcopy(CASES[0]["response"]["details"])
    data["source"].update(
        plan_sha256=plan["plan_sha256"], experiment_id=plan["experiment"]["experiment_id"]
    )
    runs = {}
    for sample, trial in zip(data["samples"], plan["trials"], strict=True):
        sample["trial_id"] = trial["trial_id"]
        run = {
            "schema_version": 3,
            "run_id": "placeholder",
            "kind": "run",
            "execution_mode": "experiment",
            "origin": "measured",
            "experiment_id": "placeholder",
            "trial_id": "placeholder",
            "plan_sha256": "a" * 64,
            "parent_run_id": None,
            "relation": "initial",
            "tool_version": "fixture",
            "tool_source_sha256": "a" * 64,
            "definition_versions": plan["experiment"]["definition_versions"],
            "resumed_case_ids": [],
            "diagnostic": False,
        }
        run.update(
            run_id=sample["run"]["run_id"],
            trial_id=trial["trial_id"],
            plan_sha256=plan["plan_sha256"],
            experiment_id=data["source"]["experiment_id"],
        )
        runs[run["run_id"]] = run
    data["summary"]["trial_ids"] = [s["trial_id"] for s in data["samples"]]
    validate_result_bindings(data, plan, runs)
    for field in ("trial_id", "experiment_id", "plan_sha256"):
        bad = deepcopy(runs)
        bad["run-0"][field] = "b" * 64
        with pytest.raises(ContractError, match="source_run_mismatch"):
            validate_result_bindings(data, plan, bad)
    bad = deepcopy(data)
    bad["samples"][0]["trial_id"] = "unrelated-trial"
    bad["summary"]["trial_ids"][0] = "unrelated-trial"
    with pytest.raises(ContractError, match="source_trial_mismatch"):
        validate_result_bindings(bad, plan, runs)


@pytest.mark.parametrize("code", [2, 3, 4, 130, True])
def test_complete_response_cannot_claim_another_exit(code):
    with pytest.raises(ContractError):
        validate_response(CASES[0]["response"], code)


def test_fixture_is_strict_json():
    assert (
        json.loads(GOLDEN.read_text(), parse_constant=lambda _: pytest.fail("nonfinite JSON"))
        == CASES
    )
