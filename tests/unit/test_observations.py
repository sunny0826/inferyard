"""Specified four-request example and contract-bound metric evidence."""

from copy import deepcopy

import pytest

from inferyard.analysis.observations import build_observations
from inferyard.analysis.scoring import score_case
from inferyard.contracts.validation import validate_document
from tests.unit.test_scoring_contract import POLICY, case, structured

RUN = {
    "origin": "measured",
    "run_id": "r1",
    "trial_id": "t1",
    "definition_versions": {
        "measurement": "phase2.v1",
        "scoring": "phase2.v1",
        "comparison": "phase2.v1",
    },
}
EVIDENCE = [{"path": "events.jsonl", "sha256": "a" * 64}]


def examples():
    cases = [case("qa", {"answers": ["yes"], "normalization": "strip"}, f"c{i}") for i in range(4)]
    rows = []
    for i, (task, answer) in enumerate(zip(cases, ["yes", "no", "yes", "yes"], strict=True)):
        rows.append(
            {
                "case_id": task["case_id"],
                "category": "qa",
                "request_id": f"request-{i}",
                "execution_state": "completed" if i < 3 else "failed",
                "error_category": None if i < 3 else "total_timeout",
                "budget_exhausted": i == 2,
                "score": score_case(task, answer, POLICY),
                "t_send_ns": 0,
                "t_first_content_ns": 200_000_000,
                "t_first_answer_ns": 300_000_000,
                "t_terminal_ns": (i + 1) * 1_000_000_000,
                "completion_tokens": 12,
                "token_source": "endpoint.usage",
                "token_scope": "completion_tokens",
            }
        )
    return cases, rows


def build(cases, rows, complete=True):
    return build_observations(RUN, "workload-1", cases, rows, EVIDENCE, complete=complete)


def metric(items, code, statistic=None, request=None, category="qa"):
    matches = [
        m
        for m in items
        if m["metric_id"] == code
        and m["request_id"] == request
        and m["group"]["category"] == category
        and (statistic is None or m["statistic"] == statistic)
    ]
    assert len(matches) == 1
    return matches[0]


def test_four_attempts_keep_failures_and_budget_exhaustion_in_denominator():
    items = build(*examples())
    for code, expected in [
        ("R01", 0.75),
        ("R02", 0.25),
        ("R03", 0.25),
        ("R04", 0.25),
        ("Q01", 0.5),
        ("Q06", 0.5),
    ]:
        item = metric(items, code)
        assert item["value"] == expected
        assert item["denominator"] == 4
        assert item["comparison_eligible"]
        assert item["evidence_refs"] == EVIDENCE
    assert metric(items, "R03")["group"]["error_category"] == "total_timeout"
    assert metric(items, "L03", "max")["value"] == 3000
    assert metric(items, "L03", "max")["excluded"] == 1
    assert metric(items, "L03", "p95")["missing_reason"] == "insufficient_samples"
    assert metric(items, "L02", "failed_first_event", "request-3")["value"] == 300
    assert not metric(items, "L02", "failed_first_event", "request-3")["comparison_eligible"]
    for item in items:
        validate_document("metric_observation", item)


def test_cancelled_attempt_never_improves_quality_or_budget_rate():
    cases, rows = examples()
    cases.append({**deepcopy(cases[0]), "case_id": "cancelled"})
    rows.append(
        {
            **deepcopy(rows[0]),
            "case_id": "cancelled",
            "request_id": "cancelled",
            "execution_state": "cancelled",
            "budget_exhausted": True,
        }
    )
    items = build(cases, rows, complete=False)
    assert metric(items, "R01")["value"] == 0.75
    assert metric(items, "R04")["numerator"] == 1
    assert metric(items, "Q01")["value"] is None
    assert metric(items, "Q01")["numerator"] == 2
    assert metric(items, "Q01")["denominator"] == 4
    assert metric(items, "Q01")["excluded"] == 1
    assert all(not m["comparison_eligible"] for m in items)


def test_structured_field_count_is_not_request_count():
    cases = [structured("s0"), structured("s1")]
    rows = [
        {
            "case_id": c["case_id"],
            "category": "structured",
            "request_id": c["case_id"],
            "execution_state": "completed",
            "score": score_case(c, answer, POLICY),
        }
        for c, answer in zip(cases, ['{"data":[false,2,null]}', "bad json"], strict=True)
    ]
    item = metric(build(cases, rows), "Q05", category="structured")
    assert (item["numerator"], item["denominator"], item["sample_count"]) == (2, 6, 2)
    assert item["value"] == pytest.approx(1 / 3)


def test_identical_numeric_values_remain_separate_by_category():
    cases, rows = examples()
    task = case(
        "math", dict(expected=1, absolute_tolerance=0, relative_tolerance=0, unit=""), "math"
    )
    cases.append(task)
    rows.append(
        {
            **rows[0],
            "case_id": "math",
            "category": "math",
            "request_id": "math",
            "score": score_case(task, "1", POLICY),
        }
    )
    items = build(cases, rows)
    assert metric(items, "Q01", category="math")["value"] == 1
    assert metric(items, "Q01")["value"] == 0.5
    assert {m["group"]["workload_id"] for m in items} == {"workload-1"}


def test_absent_attempts_have_zero_denominator_not_zero_score():
    cases, rows = examples()
    for row in rows:
        row.update(execution_state="not_executed", request_id=None)
    items = build(cases, rows, complete=False)
    assert metric(items, "R01")["value"] is None
    assert metric(items, "R01")["denominator"] == 0
    assert metric(items, "L03", "min")["value"] is None
    assert metric(items, "L03", "min")["sample_count"] == 0
