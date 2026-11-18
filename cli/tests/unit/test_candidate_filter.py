from copy import deepcopy

import pytest

from inferyard.analysis.candidate_filter import evaluate_candidate, validate_spec
from inferyard.errors import EvidenceError


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
