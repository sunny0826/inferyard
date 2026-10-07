"""P2-01/P2-03: independent count, lineage, missingness and budget expectations."""

from copy import deepcopy

import pytest
from jsonschema import Draft202012Validator

from inferyard.config.plan_math import plan_hash
from inferyard.config.planning import compile_plan
from inferyard.contracts.schemas import export_schema, schemas_for
from inferyard.contracts.validation import ContractError, Document, validate_document


def experiment():
    return {
        "schema_version": 3,
        "experiment_id": "experiment-01",
        "name": "Two repeated cases",
        "definition_versions": {"measurement": "v2", "scoring": "v1", "comparison": "v2"},
        "execution": {
            "concurrency": 1,
            "automatic_retries": 0,
            "order": "fixed",
            "seed": None,
            "service_transition": "operator_verified",
        },
        "comparison": {"mode": "model", "factor": None},
        "budget": {
            "max_requests": 100,
            "max_wall_seconds": 1000,
            "min_disk_bytes": 5 * 1024**3,
            "min_available_memory_bytes": 8 * 1024**3,
        },
        "workloads": [
            {
                "workload_id": "w1",
                "purpose": "quality",
                "config": {"path": "config.json", "sha256": "a" * 64},
                "bundle": {"path": "bundle.json", "sha256": "b" * 64},
                "protocol": {"kind": "fixed", "case_ids": ["c1", "c2"]},
                "repeats": 3,
                "timeout_seconds": 10,
                "overhead_budget_seconds": 5,
                "input_target_tokens": None,
                "output_budget_tokens": 512,
            }
        ],
    }


def observation():
    return {
        "schema_version": 3,
        "metric_id": "R01",
        "definition_version": "v2",
        "run_id": "run-1",
        "trial_id": "trial-1",
        "request_id": None,
        "group": {"workload_id": None, "category": None, "error_category": None},
        "statistic": "completion_rate",
        "value": 0.75,
        "unit": "ratio",
        "layer": "experiment",
        "source": "events.jsonl",
        "status": "derived",
        "missing_reason": None,
        "sample_count": 4,
        "numerator": 3,
        "denominator": 4,
        "excluded": 0,
        "sampled_start_ns": None,
        "sampled_end_ns": None,
        "coverage_ratio": None,
        "comparison_eligible": True,
        "limitations": [],
        "evidence_refs": [{"path": "events.jsonl", "sha256": "c" * 64}],
    }


def test_repetitions_are_trials_not_new_cases_and_have_exact_budget():
    source = experiment()
    original = deepcopy(source)
    plan = compile_plan(source)
    assert source == original
    assert len(plan["trials"]) == 3
    assert {t["repeat_index"] for t in plan["trials"]} == {0, 1, 2}
    assert all(t["case_order"] == ["c1", "c2"] for t in plan["trials"])
    assert plan["request_limit"] == 6
    assert plan["request_budget_seconds"] == 60
    assert plan["total_budget_seconds"] == 75
    assert compile_plan(source) == plan
    Draft202012Validator(export_schema("plan")).validate(plan)
    document = Document.parse("plan", plan)
    plan["experiment"]["name"] = "mutated"
    assert document.to_dict()["experiment"]["name"] != "mutated"


def test_duration_upper_request_limit_is_not_a_fixed_case_count():
    source = experiment()
    workload = source["workloads"][0]
    workload.update(purpose="stability", repeats=1)
    workload["protocol"] = {
        "kind": "duration",
        "case_ids": ["c1", "c2"],
        "duration_seconds": 60,
        "max_requests": 50,
        "window_seconds": 20,
        "min_completed_per_case_per_window": 2,
        "drain_timeout_seconds": 10,
    }
    plan = compile_plan(source)
    assert plan["request_limit"] == 50
    assert plan["request_budget_seconds"] == 500
    assert plan["total_budget_seconds"] == 75  # 60 send + 10 drain + 5 overhead
    assert plan["trials"][0]["case_order"] == ["c1", "c2"]


@pytest.mark.parametrize("field,value", [("max_requests", 5), ("max_wall_seconds", 74)])
def test_insufficient_frozen_budget_is_rejected(field, value):
    source = experiment()
    source["budget"][field] = value
    with pytest.raises(ContractError, match="budget"):
        compile_plan(source)


def test_seeded_order_reproducible_and_hash_changes_with_protocol():
    source = experiment()
    source["execution"].update(order="seeded", seed=1)
    plan = compile_plan(source)
    assert compile_plan(source) == plan
    assert all(set(t["case_order"]) == {"c1", "c2"} for t in plan["trials"])
    source["execution"]["seed"] = 2
    assert compile_plan(source)["plan_sha256"] != plan["plan_sha256"]


def test_no_implicit_retry_or_concurrency_and_no_secret_echo():
    for field in ("concurrency", "automatic_retries"):
        source = experiment()
        source["execution"][field] = 2
        with pytest.raises(ContractError):
            compile_plan(source)
    source = experiment()
    source["api_key"] = "sensitive-test-value"
    with pytest.raises(ContractError) as error:
        compile_plan(source)
    assert "sensitive-test-value" not in str(error.value)


@pytest.mark.parametrize("path", ["../outside", "/absolute", "x/../../outside", "x\\outside"])
def test_plan_references_cannot_escape_artifact_root(path):
    source = experiment()
    source["workloads"][0]["config"]["path"] = path
    with pytest.raises(ContractError):
        compile_plan(source)


def test_repetition_ledger_cannot_drop_duplicate_or_reorder_cases():
    for mode in ("drop", "duplicate", "reorder", "budget", "hash"):
        plan = compile_plan(experiment())
        if mode == "drop":
            plan["trials"].pop()
        elif mode == "duplicate":
            plan["trials"][1]["repeat_index"] = 0
        elif mode == "reorder":
            plan["trials"][0]["case_order"].reverse()
        elif mode == "budget":
            plan["total_budget_seconds"] = 1
        else:
            plan["plan_sha256"] = "f" * 64
        if mode != "hash":
            plan["plan_sha256"] = plan_hash(plan)
        with pytest.raises(ContractError):
            validate_document("plan", plan)


def test_missing_zero_rates_and_intervals_are_separate():
    data = observation()
    validate_document("metric_observation", data)
    data.update(value=0, numerator=0)
    validate_document("metric_observation", data)
    data.update(
        value=None, status="missing", missing_reason="incomplete_run", comparison_eligible=False
    )
    validate_document("metric_observation", data)
    data["comparison_eligible"] = True
    with pytest.raises(ContractError):
        validate_document("metric_observation", data)


@pytest.mark.parametrize(
    "changes",
    [
        {"value": 0.5},
        {"denominator": 0},
        {"numerator": 5},
        {"value": True},
        {"value": float("inf")},
        {"value": float("nan")},
        {"sample_count": 0},
        {"evidence_refs": []},
        {"sampled_start_ns": 20, "sampled_end_ns": 10},
        {"coverage_ratio": 0.5},
        {"status": "missing"},
    ],
)
def test_invalid_observations_do_not_create_plausible_numbers(changes):
    data = observation()
    data.update(changes)
    with pytest.raises(ContractError):
        validate_document("metric_observation", data)


def test_resume_lineage_requires_new_run_and_explicit_subset():
    data = {
        "schema_version": 3,
        "run_id": "r2",
        "kind": "run",
        "execution_mode": "experiment",
        "origin": "measured",
        "experiment_id": "e1",
        "trial_id": "t1",
        "plan_sha256": "a" * 64,
        "parent_run_id": "r1",
        "relation": "resume",
        "tool_version": "0.2.0",
        "tool_source_sha256": "b" * 64,
        "definition_versions": experiment()["definition_versions"],
        "resumed_case_ids": ["c2"],
        "diagnostic": False,
    }
    validate_document("run", data)
    for changes in ({"parent_run_id": "r2"}, {"resumed_case_ids": []}, {"relation": "initial"}):
        with pytest.raises(ContractError):
            validate_document("run", {**data, **changes})


def test_unsupported_or_boolean_versions_fail_closed():
    for version in (True, 0, 1, 2, 4, "3"):
        source = experiment()
        source["schema_version"] = version
        with pytest.raises(ContractError):
            validate_document("experiment", source)
    for kind in schemas_for():
        Draft202012Validator.check_schema(export_schema(kind))


def test_analysis_cannot_claim_a_metric_from_an_unbound_run():
    data = {
        "schema_version": 3,
        "analysis_id": "analysis-1",
        "source_runs": [{"run_id": "run-1", "manifest_sha256": "a" * 64}],
        "parent_analysis_id": None,
        "reason": "independent scorer revision",
        "definition_versions": experiment()["definition_versions"],
        "scorer_id": "scorer-v2",
        "scorer_sha256": "b" * 64,
        "answer_policy_sha256": "c" * 64,
        "metrics": [observation()],
        "limitations": [],
    }
    validate_document("analysis", data)
    data["metrics"][0]["run_id"] = "unrelated-run"
    with pytest.raises(ContractError, match="unknown source"):
        validate_document("analysis", data)


def test_optional_turbo_safety_condition_is_frozen_and_strict():
    source = experiment()
    source["safety"] = dict(
        interval_seconds=1,
        max_temperature_celsius=90,
        require_temperature=True,
        max_external_cpu_percent=None,
        check_environment=True,
    )
    old = compile_plan(source)
    source["safety"]["intel_pstate_no_turbo"] = 1
    disabled = compile_plan(source)
    source["safety"]["intel_pstate_no_turbo"] = 0
    enabled = compile_plan(source)
    assert len({p["plan_sha256"] for p in (old, disabled, enabled)}) == 3
    assert "intel_pstate_no_turbo" not in old["experiment"]["safety"]
    for value in (None, True, -1, 2, "1"):
        source["safety"]["intel_pstate_no_turbo"] = value
        with pytest.raises(ContractError):
            compile_plan(source)
