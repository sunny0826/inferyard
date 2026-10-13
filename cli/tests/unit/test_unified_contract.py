"""Unique wire entry points and denominator rules across execution modes."""

from copy import deepcopy

import pytest
from jsonschema import Draft202012Validator

from inferyard import SCHEMA_VERSION
from inferyard.contracts.schemas import export_schema, schemas_for
from inferyard.contracts.validation import ContractError, Document, validate_document


def _rate(n=0, d=0, excluded=0):
    return dict(
        numerator=n,
        denominator=d,
        excluded=excluded,
        value=n / d if d else None,
        reason=None if d else "no_valid_samples",
    )


def _distribution(excluded=1):
    return dict(
        sample_count=0,
        excluded=excluded,
        min=None,
        p50=None,
        p95=None,
        max=None,
        quantile_method="nearest_rank.phase2.v1",
        p95_reason="insufficient_samples",
        p95_exploratory=False,
    )


def summary(*, duration=False):
    """One attempted performance request failed; failure remains in the denominator."""
    quality = {
        "Q01": {},
        **{code: _rate() for code in ("Q02", "Q03", "Q04", "Q05", "Q06", "Q07")},
        "Q08": dict(
            value=None,
            reason="no_classification_samples",
            sample_count=0,
            classes=[],
            zero_division=0,
            confusion=[],
        ),
    }
    context = {
        "environment_qualification": dict(
            definition="observed_environment_qualification.v1",
            eligible=False,
            reasons=["missing"],
            request_count=1,
            limitations=[],
        ),
        "external_cpu": dict(
            source="fixture",
            intervals=[],
            sample_count=0,
            valid_intervals=0,
            observed_max_percent=None,
            comparison_eligible=False,
            limitations=[],
        ),
        "external_cpu_requests": dict(
            policy=None, requests=[], all_requests_eligible=False, limitations=[]
        ),
        "environment": dict(
            cpu_policies=dict(complete_and_stable=False, reasons=["missing"], snapshot_count=0),
            stable_observed_environment=False,
            reasons=["missing"],
            observation_count=0,
            largest_request_gap_ns=None,
            limitations=[],
            comparison_eligible=False,
        ),
        "collector_schedule": dict(
            periodic_samples=0,
            boundary_samples=0,
            max_work_ns=None,
            max_late_ns=None,
            work_exceeds_interval_count=0,
            boundary_work_ns=0,
            overhead_gate="not_verified",
            limitations=[],
        ),
        "evidence_refs": [],
        "performance_comparison_eligible": False,
    }
    result = {
        "schema_version": SCHEMA_VERSION,
        "run_id": "r1",
        "trial_id": "t1",
        "experiment_id": "e1",
        "protocol_kind": "duration" if duration else "fixed",
        "completeness": "incomplete",
        "scope_complete": False,
        "evidence_complete": True,
        "stop_reason": "interrupted",
        "execution_parameters": dict(request_timeout_seconds=10, output_budget_tokens=16),
        "counts": dict(
            planned=1,
            executed=1,
            valid_executed=1,
            completed=0,
            failed=1,
            cancelled=0,
            invalid=0,
            not_executed=0,
            budget_exhausted=0,
            budget_exhausted_completed=0,
            budget_exhausted_other_diagnostic=0,
        ),
        "completion_rate": _rate(0, 1),
        "quality": quality,
        "performance": {
            "performance": {
                "metrics": {
                    **{
                        code: {
                            **_distribution(),
                            "unit": "token/s" if code in ("L04", "L06", "L07") else "ms",
                            "missing_reasons": {},
                        }
                        for code in ("L01", "L02", "L03", "L04", "L06", "L07")
                    },
                    "L05": [],
                },
                "planned": 1,
                "completed": 0,
                "failed": 1,
                "timeout_count": 1,
                "failure_categories": {"total_timeout": 1},
                "failed_first_events": [],
            }
        },
        "limitations": [],
        "metric_observations": [],
        "resources": dict(baseline_rss_bytes=None, requests=[]),
        "input_target_check": dict(
            status="not_requested", target_tokens=None, tolerance_tokens=0, cases=[]
        ),
        "input_lengths": dict(bins=[], limitations=[]),
        "measurement_context": context,
    }
    if duration:
        result["counts"].update(planned=None, request_limit=10)
        result["quality"] = dict(status="repeated_probe_observations_only", independent_cases=1)
        result["idle_rss"] = dict(points=[], first_cycle_rss_bytes=None)
        result["duration"] = {
            "start_ns": 0,
            "admission_deadline_ns": 100,
            "drain_deadline_ns": 200,
            "request_limit": 10,
            "admitted_requests": 1,
            "window_completed": False,
            "reason": "interrupted",
            "closed_ns": 50,
            "independent_cases": 1,
            "limitations": [],
            "probe_coverage_complete": False,
            "windows": [
                {
                    "index": 0,
                    "start_ns": 0,
                    "end_ns": 100,
                    "full_width": True,
                    "cases": [
                        dict(
                            case_id="c1",
                            started=1,
                            completed_in_send_cohort=0,
                            unfinished_or_invalid_timing=0,
                            cross_boundary_completed=0,
                            failed=1,
                            valid_executed=1,
                            execution_states={"failed": 1},
                            median_latency_ns=None,
                            baseline_latency_ns=None,
                            latency_drift_ratio=None,
                            latency_difference_ns=None,
                            missing_reason="insufficient_completed_per_case",
                        )
                    ],
                }
            ],
        }
    return result


def test_registry_exposes_one_revision_and_no_version_switch():
    assert SCHEMA_VERSION == 3
    for kind in schemas_for():
        exported = export_schema(kind)
        Draft202012Validator.check_schema(exported)
        assert exported["$id"].startswith("urn:local-ai-bench:schema:v3:")
    with pytest.raises(TypeError):
        schemas_for(2)
    with pytest.raises(TypeError):
        export_schema("bundle", 2)


@pytest.mark.parametrize("revision", [1, 2, True, "3", None])
def test_active_parser_never_accepts_old_or_implicit_revision(revision):
    data = summary()
    data["schema_version"] = revision
    with pytest.raises(ContractError, match="schema_version"):
        Document.parse("summary", data)


@pytest.mark.parametrize("duration", [False, True])
def test_fixed_and_duration_keep_failed_request_in_denominator(duration):
    data = summary(duration=duration)
    Draft202012Validator(export_schema("summary")).validate(data)
    saved = Document.parse("summary", data)
    assert saved.to_dict()["completion_rate"] == _rate(0, 1)
    assert saved.to_dict()["counts"]["planned"] == (None if duration else 1)
    data["counts"]["failed"] = 0
    assert saved.to_dict()["counts"]["failed"] == 1
    with pytest.raises(ContractError):
        validate_document("summary", data)


@pytest.mark.parametrize(
    "changes",
    [
        {"planned": 2},
        {"planned": None},
        {"valid_executed": 0},
        {"failed": True},
        {"budget_exhausted": 2},
        {"request_limit": 10},
    ],
)
def test_fixed_summary_rejects_changed_or_removed_denominator(changes):
    data = summary()
    data["counts"].update(changes)
    with pytest.raises(ContractError):
        validate_document("summary", data)


@pytest.mark.parametrize(
    "change", ["zero_planned", "wrong_limit", "admission", "early_complete", "cohort", "probe"]
)
def test_duration_does_not_turn_window_limits_into_planned_cases(change):
    data = summary(duration=True)
    if change == "zero_planned":
        data["counts"]["planned"] = 0
    elif change == "wrong_limit":
        data["counts"]["request_limit"] = 2
    elif change == "admission":
        data["duration"]["admitted_requests"] = 2
    elif change == "early_complete":
        data["duration"]["window_completed"] = True
    elif change == "cohort":
        data["duration"]["windows"][0]["cases"][0]["execution_states"] = {}
    else:
        data["quality"]["independent_cases"] = 2
    with pytest.raises(ContractError):
        validate_document("summary", data)


def test_unknown_nested_summary_fields_and_fabricated_metric_samples_are_rejected():
    data = summary()
    data["execution_parameters"]["unfrozen"] = 1
    with pytest.raises(ContractError, match="unknown field"):
        validate_document("summary", data)
    data = summary()
    data["performance"]["performance"]["metrics"]["L03"].update(sample_count=1, min=0, p50=0, max=0)
    with pytest.raises(ContractError):
        validate_document("summary", data)


def test_run_records_require_explicit_kind_mode_and_measurement_origin():
    data = dict(
        schema_version=3,
        run_id="r1",
        experiment_id="e1",
        trial_id="t1",
        kind="run",
        execution_mode="single",
        origin="measured",
        plan_sha256="a" * 64,
        parent_run_id=None,
        relation="initial",
        tool_version="test",
        tool_source_sha256="b" * 64,
        definition_versions=dict(measurement="v2", scoring="v1", comparison="v2"),
        resumed_case_ids=[],
        diagnostic=False,
    )
    validate_document("run", data)
    for key in ("kind", "execution_mode", "origin"):
        changed = deepcopy(data)
        del changed[key]
        with pytest.raises(ContractError, match="required field"):
            validate_document("run", changed)
    rerun = {**data, "relation": "rerun", "parent_run_id": "prior", "origin": "migrated"}
    validate_document("run", rerun)


def test_manifest_paths_and_derived_classification_are_not_data_owned():
    data = dict(
        schema_version=3,
        run_id="r1",
        sealed=True,
        files={"events.jsonl": dict(sha256="a" * 64, bytes=12, derived=False)},
    )
    validate_document("manifest", data)
    data["files"]["events.jsonl"]["derived"] = True
    with pytest.raises(ContractError, match="classification"):
        validate_document("manifest", data)
    data["files"] = {"../events.jsonl": dict(sha256="a" * 64, bytes=12, derived=False)}
    with pytest.raises(ContractError, match="relative"):
        validate_document("manifest", data)
