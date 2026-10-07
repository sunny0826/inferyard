from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.environment import FIELDS
from tests.unit.test_comparison import trial


def inputs():
    left, right = trial(), trial()
    proofs = []
    for index, data in enumerate((left, right)):
        data["run"]["run_id"] = str(index)
        data["environment_start"].update(dict.fromkeys((*FIELDS, "cpu_policies"), "known"))
        data["plan"]["experiment"]["performance_environment"] = {
            "max_external_cpu_percent": 25,
            "max_external_interval_seconds": 1,
        }
        data["summary"]["measurement_context"] = {"environment_qualification": {"eligible": True}}
        data["requests"] = [
            {
                "request_id": "r" + str(index),
                "case_id": "case",
                "category": "qa",
                "execution_state": "completed",
                "token_source": "endpoint.usage",
                "token_scope": "completion_tokens",
            }
        ]
        data["summary"]["metric_observations"] = [
            {
                "metric_id": code,
                "statistic": "p50",
                "group": {"category": "qa"},
                "request_id": None,
                "source": "same",
                "layer": "client",
                "definition_version": "v2",
                "unit": "token/s" if code == "L04" else "ms",
                "value": 100 + 20 * index,
                "missing_reason": None,
                "sample_count": 1,
                "excluded": 0,
                "comparison_eligible": False,
                "limitations": ["performance_comparison_gate_pending"],
            }
            for code in ("L01", "L03", "L04")
        ]
        proofs.append(
            {
                "eligible": True,
                "reasons": [],
                "target_run_id": str(index),
                "tolerance_ratio": 0.05,
                "total_observer_binding": {"eligible": True, "evidence_kind": "unit_fixture"},
            }
        )
    return left, right, proofs


def test_context_grants_only_validated_metrics_without_mutating_source():
    left, right, proofs = inputs()
    original = deepcopy((left, right))
    result = compare_trials(left, right, performance_evidence=proofs)
    assert result["eligibility"]["performance"]
    rows = {r["metric_id"]: r for r in result["performance_analysis"]["differences"]}
    assert rows["L03"]["difference"] == 20
    assert rows["L04"]["difference"] == 20
    assert rows["L01"]["difference"] is None and not rows["L01"]["eligible"]
    assert (left, right) == original
    assert not compare_trials(left, right)["eligibility"]["performance"]


@pytest.mark.parametrize(
    "change", ["overhead", "binding", "environment", "tolerance", "incomplete", "side_by_side"]
)
def test_failed_prerequisite_cannot_produce_any_performance_delta(change):
    left, right, proofs = inputs()
    mode = None
    if change == "overhead":
        proofs[1].update(eligible=False, reasons=["not_before_target"])
    elif change == "binding":
        proofs[1]["target_run_id"] = "wrong"
    elif change == "environment":
        right["environment_start"]["cpu_policies"] = "different"
    elif change == "tolerance":
        proofs[1]["tolerance_ratio"] = 0.1
    elif change == "incomplete":
        right["summary"]["completeness"] = "incomplete"
    else:
        mode = "side-by-side"
    result = compare_trials(left, right, mode=mode, performance_evidence=proofs)
    assert not result["eligibility"]["performance"]
    assert result["performance_analysis"]["blockers"]
    assert all(r["difference"] is None for r in result["performance_analysis"]["differences"])


def test_different_model_tokenizer_blocks_rate_but_not_task_e2e():
    left, right, proofs = inputs()
    right["config"]["model"]["sha256"] = "different-model"
    rows = {
        r["metric_id"]: r
        for r in compare_trials(left, right, performance_evidence=proofs)["performance_analysis"][
            "differences"
        ]
    }
    assert rows["L03"]["eligible"]
    assert rows["L04"]["difference"] is None
    assert "tokenizer_identity_not_proven_equal" in rows["L04"]["reasons"]


@pytest.mark.parametrize("change", ["missing", "definition", "cohort", "duplicate", "nonfinite"])
def test_bad_individual_metric_remains_ineligible(change):
    left, right, proofs = inputs()
    metric = right["summary"]["metric_observations"][1]
    if change == "missing":
        metric["value"] = None
    elif change == "nonfinite":
        metric["value"] = float("nan")
    elif change == "definition":
        metric["definition_version"] = "other"
    elif change == "cohort":
        metric["sample_count"] = 0
    else:
        right["summary"]["metric_observations"].append(deepcopy(metric))
    rows = compare_trials(left, right, performance_evidence=proofs)["performance_analysis"][
        "differences"
    ]
    row = next(r for r in rows if r["metric_id"] == "L03")
    assert not row["eligible"] and row["difference"] is None


def test_first_event_requires_both_predeclared_metric_assessments():
    left, right, proofs = inputs()
    for p in proofs:
        p["assessment"] = {
            "first_event_assessments": {
                "L01": {
                    "passed": True,
                    "definition": "first_event_abba.v1",
                    "tolerance_ratio": 0.05,
                }
            }
        }

    def row():
        return next(
            r
            for r in compare_trials(left, right, performance_evidence=proofs)[
                "performance_analysis"
            ]["differences"]
            if r["metric_id"] == "L01"
        )

    assert row()["eligible"] and row()["difference"] == 20
    proofs[1]["assessment"]["first_event_assessments"]["L01"]["tolerance_ratio"] = 0.1
    assert not row()["eligible"]
    proofs[1]["assessment"]["first_event_assessments"]["L01"].update(
        tolerance_ratio=0.05, passed=False
    )
    assert not row()["eligible"]
    proofs[1].pop("assessment")
    assert not row()["eligible"]
