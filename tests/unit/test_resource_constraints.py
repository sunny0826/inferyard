from copy import deepcopy

import pytest

from inferyard.analysis.candidate_filter import evaluate_candidate, validate_spec
from inferyard.analysis.comparison import compare_trials
from inferyard.evidence.storage import EvidenceError
from tests.unit.test_resource_comparison import fixture, refresh


def inputs():
    left, right, proofs = fixture()
    for data in (left, right):
        original = data["requests"][0]
        other = {
            **original,
            "request_id": original["request_id"] + "-2",
            "case_id": "other",
            "category": "extraction",
        }
        data["requests"].append(other)
        data["samples"].extend(
            {**s, "request_id": other["request_id"]}
            for s in list(data["samples"])
            if s["request_id"] == original["request_id"]
        )
        for sample in data["samples"]:
            if (
                sample["metric_name"] == "service_rss"
                and sample["request_id"] == other["request_id"]
            ):
                sample["value"] = 120
        refresh(data)
    metric = next(m for m in right["summary"]["metric_observations"] if m["metric_id"] == "C02")
    constraint = {
        k: metric[k] for k in ("metric_id", "statistic", "source", "unit", "definition_version")
    }
    constraint.update(
        category=None, error_category=None, operator="<=", threshold=120, aggregation="all_requests"
    )
    return left, right, proofs, constraint


def evaluate(left, right, proofs, constraint):
    comparison = compare_trials(left, right, performance_evidence=proofs)
    return evaluate_candidate(right, comparison, [constraint])


def test_all_requests_upper_max_lower_min_and_category_scope():
    left, right, proofs, c = inputs()
    before = deepcopy(right)
    result = evaluate(left, right, proofs, c)
    row = result["constraints"][0]
    assert result["matched"] and row["value"] == 120
    assert row["aggregation"]["planned_requests"] == 2
    assert row["aggregation"]["counts"] == {"pass": 2, "fail": 0, "unknown": 0}
    assert not evaluate(left, right, proofs, {**c, "threshold": 119})["matched"]
    lower = evaluate(left, right, proofs, {**c, "operator": ">=", "threshold": 100})["constraints"][
        0
    ]
    assert lower["status"] == "pass" and lower["value"] == 100
    selected = evaluate(left, right, proofs, {**c, "category": "qa", "threshold": 100})[
        "constraints"
    ][0]
    assert selected["status"] == "pass" and selected["aggregation"]["planned_requests"] == 1
    assert selected["value"] == 100 and right == before


@pytest.mark.parametrize(
    "change",
    [
        "missing",
        "duplicate",
        "unexecuted",
        "failed",
        "unqualified",
        "incomplete",
        "duplicate_case",
        "nonfinite",
        "missing_reason",
    ],
)
def test_unknown_slots_stay_in_denominator_and_never_pass(change):
    left, right, proofs, c = inputs()
    comparison = compare_trials(left, right, performance_evidence=proofs)
    m = next(m for m in right["summary"]["metric_observations"] if m["metric_id"] == "C02")
    if change == "missing":
        right["summary"]["metric_observations"].remove(m)
    elif change == "duplicate":
        right["summary"]["metric_observations"].append(deepcopy(m))
    elif change == "unexecuted":
        right["requests"][0].update(request_id=None, execution_state="not_executed")
    elif change == "failed":
        right["requests"][0]["execution_state"] = "failed"
    elif change == "unqualified":
        next(
            r for r in comparison["performance_analysis"]["differences"] if r["metric_id"] == "C02"
        )["eligible"] = False
    elif change == "incomplete":
        right["summary"]["completeness"] = "incomplete"
    elif change == "duplicate_case":
        right["requests"][1]["case_id"] = right["requests"][0]["case_id"]
    elif change == "nonfinite":
        m["value"] = float("inf")
    else:
        m["missing_reason"] = "missing"
    result = evaluate_candidate(right, comparison, [c])
    row = result["constraints"][0]
    assert not result["matched"] and row["value"] is None
    assert (
        row["aggregation"]["planned_requests"] == 2 and row["aggregation"]["counts"]["unknown"] > 0
    )


def test_known_failure_is_preserved_when_another_request_is_unknown():
    left, right, proofs, c = inputs()
    comparison = compare_trials(left, right, performance_evidence=proofs)
    m = next(
        m
        for m in right["summary"]["metric_observations"]
        if m["metric_id"] == "C02" and m["group"]["category"] == "qa"
    )
    right["summary"]["metric_observations"].remove(m)
    result = evaluate_candidate(right, comparison, [{**c, "threshold": 110}])
    row = result["constraints"][0]
    assert result["status"] == "fail" and not result["matched"]
    assert row["aggregation"]["counts"] == {"pass": 0, "fail": 1, "unknown": 1}
    assert row["value"] is None


def test_empty_scope_and_no_link_are_unknown():
    left, right, proofs, c = inputs()
    assert evaluate(left, right, proofs, {**c, "category": "not-present"})["status"] == "unknown"
    assert evaluate_candidate(right, compare_trials(left, right), [c])["status"] == "unknown"


def test_schema_requires_explicit_resource_aggregation():
    *_, c = inputs()
    spec = dict(version=1, reference="a", candidates=["b"], constraints=[c])
    validate_spec(spec)
    for bad in (
        {**c, "aggregation": "sum"},
        {**c, "metric_id": "L05"},
        {**c, "error_category": "timeout"},
    ):
        with pytest.raises(EvidenceError):
            validate_spec({**spec, "constraints": [bad]})
