from copy import deepcopy

import pytest

from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import ContractError
from inferyard.platforms.external_cpu_windows import assess_windows
from tests.unit.test_phase2_contracts import experiment

POLICY = {"max_external_cpu_percent": 10, "max_external_interval_seconds": 1}
REQUEST = {
    "request_id": "r",
    "execution_state": "completed",
    "t_send_ns": 100,
    "t_terminal_ns": 900,
}


def interval(left, right, value):
    return {"start_ns": left, "end_ns": right, "value_percent": value}


def assess(rows, policy=POLICY, request=REQUEST):
    return assess_windows({"intervals": rows}, [request], policy)


def test_bracketing_intervals_cover_request_without_double_counting():
    result = assess([interval(0, 500, 0), interval(500, 1000, 10)])
    row = result["requests"][0]
    assert row["covered_ns"] == 800 and row["coverage_ratio"] == 1
    assert row["observed_max_percent"] == 10 and result["all_requests_eligible"]
    overlapping = assess([interval(0, 1000, 0), interval(500, 1000, 0)])
    assert overlapping["requests"][0]["coverage_ratio"] == 1


@pytest.mark.parametrize(
    "rows,reason",
    [
        ([interval(200, 1000, 0)], "external_load_request_coverage_incomplete"),
        (
            [interval(0, 500, 0), interval(500, 1000, None)],
            "external_load_request_coverage_incomplete",
        ),
        ([interval(0, 1000, 11)], "external_load_frozen_tolerance_exceeded"),
        ([interval(0, 2_000_000_000, 0)], "external_load_interval_too_wide"),
    ],
)
def test_missing_excess_and_wide_intervals_do_not_qualify(rows, reason):
    result = assess(rows)
    assert not result["all_requests_eligible"]
    assert reason in result["requests"][0]["reasons"]


def test_no_frozen_tolerance_or_no_request_interval_never_qualifies():
    assert not assess([interval(0, 1000, 0)], None)["all_requests_eligible"]
    assert not assess([], request={**REQUEST, "t_terminal_ns": None})["all_requests_eligible"]
    assert not assess_windows({"intervals": []}, [], POLICY)["all_requests_eligible"]


def test_performance_tolerance_changes_plan_hash_and_invalid_bounds_rejected():
    source = experiment()
    original = compile_plan(source)["plan_sha256"]
    source["performance_environment"] = POLICY
    assert compile_plan(source)["plan_sha256"] != original
    for field, value in [
        ("max_external_cpu_percent", -1),
        ("max_external_cpu_percent", 101),
        ("max_external_interval_seconds", 0),
    ]:
        invalid = deepcopy(source)
        invalid["performance_environment"][field] = value
        with pytest.raises(ContractError):
            compile_plan(invalid)
