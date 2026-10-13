from copy import deepcopy

import pytest

from inferyard.analysis.candidate_filter import evaluate_candidate, validate_spec
from inferyard.evidence.storage import EvidenceError


def fixture():
    constraint = {
        "metric_id": "Q01",
        "statistic": "pass_rate",
        "category": "qa",
        "error_category": None,
        "unit": "ratio",
        "source": "scores",
        "definition_version": "phase2.v1",
        "operator": ">=",
        "threshold": 0.8,
    }
    metric = {
        k: v
        for k, v in constraint.items()
        if k not in ("category", "error_category", "operator", "threshold")
    }
    metric.update(group={"category": "qa"}, request_id=None, value=0.8, comparison_eligible=True)
    data = {"summary": {"completeness": "complete", "metric_observations": [metric]}}
    comparison = {"eligibility": {"quality": True, "completion": True, "performance": False}}
    return constraint, data, comparison


def test_threshold_inclusive_and_failures_retained():
    constraint, data, comparison = fixture()
    assert evaluate_candidate(data, comparison, [constraint])["matched"]
    data["summary"]["metric_observations"][0]["value"] = 0.79
    result = evaluate_candidate(data, comparison, [constraint])
    assert not result["matched"] and result["status"] == "fail"
    assert result["constraints"][0]["value"] == 0.79
    constraint.update(operator="<=", threshold=0.79)
    assert evaluate_candidate(data, comparison, [constraint])["matched"]


@pytest.mark.parametrize(
    "change,reason",
    [
        ("incomplete", "incomplete_trial"),
        ("missing", "metric_missing"),
        ("duplicate", "ambiguous_metric"),
        ("null", "metric_value_unknown"),
        ("metric_gate", "metric_not_comparable"),
        ("pair_gate", "candidate_not_comparable_to_reference"),
        ("unit", "metric_missing"),
        ("request", "metric_missing"),
    ],
)
def test_unknown_and_incomparable_never_pass(change, reason):
    constraint, data, comparison = fixture()
    metrics = data["summary"]["metric_observations"]
    if change == "incomplete":
        data["summary"]["completeness"] = "incomplete"
    elif change == "missing":
        metrics.clear()
    elif change == "duplicate":
        metrics.append(deepcopy(metrics[0]))
    elif change == "null":
        metrics[0]["value"] = None
    elif change == "metric_gate":
        metrics[0]["comparison_eligible"] = False
    elif change == "pair_gate":
        comparison["eligibility"]["quality"] = False
    elif change == "unit":
        metrics[0]["unit"] = "percent"
    elif change == "request":
        metrics[0]["request_id"] = "one-request"
    result = evaluate_candidate(data, comparison, [constraint])
    assert not result["matched"] and result["status"] == "unknown"
    assert result["constraints"][0]["reason"] == reason


def test_spec_requires_finite_threshold_and_nonempty_constraints():
    constraint, _, _ = fixture()
    spec = {"version": 1, "reference": "run", "candidates": [], "constraints": [constraint]}
    validate_spec(spec)
    for invalid in (float("nan"), float("inf"), True, 10**400):
        constraint["threshold"] = invalid
        with pytest.raises(EvidenceError):
            validate_spec(spec)
    spec["constraints"] = []
    with pytest.raises(EvidenceError):
        validate_spec(spec)


def performance_fixture():
    constraint, data, comparison = fixture()
    constraint.update(
        metric_id="L03", statistic="p50", unit="ms", source="client", operator="<=", threshold=100
    )
    metric = data["summary"]["metric_observations"][0]
    metric.update({k: constraint[k] for k in ("metric_id", "statistic", "unit", "source")})
    metric.update(value=90, comparison_eligible=False)
    comparison["eligibility"]["performance"] = True
    row = {k: metric[k] for k in ("metric_id", "statistic", "unit", "source")}
    row.update(category="qa", case_id=None, right=90, eligible=True)
    comparison["performance_analysis"] = {
        "prerequisites_eligible": True,
        "blockers": [],
        "differences": [row],
    }
    return constraint, data, comparison


def test_contextual_qualification_does_not_rewrite_source():
    c, d, p = performance_fixture()
    original = deepcopy(d)
    assert evaluate_candidate(d, p, [c])["matched"]
    assert d == original
    c["threshold"] = 89
    assert evaluate_candidate(d, p, [c])["status"] == "fail"


@pytest.mark.parametrize(
    "change",
    [
        "refused",
        "other_metric",
        "request",
        "value",
        "duplicate",
        "blocker",
        "prerequisite",
        "unit",
        "source",
    ],
)
def test_pair_permission_never_replaces_metric_permission(change):
    c, d, p = performance_fixture()
    # Even locally eligible rows need this exact pair's qualification.
    d["summary"]["metric_observations"][0]["comparison_eligible"] = True
    a = p["performance_analysis"]
    r = a["differences"][0]
    if change == "refused":
        r["eligible"] = False
    elif change == "other_metric":
        r["metric_id"] = "L04"
    elif change == "request":
        r["case_id"] = "one"
    elif change == "value":
        r["right"] = 91
    elif change == "duplicate":
        a["differences"].append(deepcopy(r))
    elif change == "blocker":
        a["blockers"] = ["environment"]
    elif change == "prerequisite":
        a["prerequisites_eligible"] = False
    elif change == "unit":
        r["unit"] = "s"
    elif change == "source":
        r["source"] = "engine"
    assert evaluate_candidate(d, p, [c])["status"] == "unknown"


def test_link_cannot_name_an_absent_candidate():
    c, _, _ = fixture()
    with pytest.raises(EvidenceError, match="unknown_candidate"):
        validate_spec(
            {
                "version": 1,
                "reference": "r",
                "candidates": [],
                "constraints": [c],
                "comparisons": {"missing": "pair"},
            }
        )
