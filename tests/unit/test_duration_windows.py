import pytest

from inferyard.analysis.duration_windows import summarize_windows
from inferyard.evidence.storage import EvidenceError

P = {"window_seconds": 1, "duration_seconds": 2.5, "min_completed_per_case_per_window": 1}


def row(key, case, start, end, state="completed"):
    return {
        "request_id": key,
        "case_id": case,
        "t_send_ns": int(start * 1e9),
        "t_terminal_ns": int(end * 1e9),
        "execution_state": state,
    }


def test_case_local_baseline_includes_cross_boundary_latency():
    result = summarize_windows(
        P,
        ["fast", "slow"],
        0,
        [
            row("a", "fast", 0, 0.1),
            row("b", "slow", 0.1, 0.5),
            row("c", "fast", 0.9, 1.1),
            row("d", "fast", 1.1, 1.3),
            row("e", "slow", 1.3, 1.7),
        ],
    )
    first, second, tail = result["windows"]
    assert first["cases"][0]["cross_boundary_completed"] == 1
    assert first["cases"][0]["median_latency_ns"] == 150_000_000
    assert second["cases"][0]["latency_drift_ratio"] == pytest.approx(1 / 3)
    assert second["cases"][1]["latency_drift_ratio"] == 0
    assert result["independent_cases"] == 2
    assert not tail["full_width"]
    assert all(c["median_latency_ns"] is None for c in tail["cases"])


def test_missing_first_window_never_borrows_later_baseline():
    result = summarize_windows(P, ["a"], 0, [row("a1", "a", 1, 1.2)])
    second = result["windows"][1]["cases"][0]
    assert second["median_latency_ns"] == 200_000_000
    assert second["latency_drift_ratio"] is None
    assert second["missing_reason"] == "initial_window_baseline_unavailable"


def test_failed_requests_count_but_do_not_supply_latency():
    result = summarize_windows(P, ["a"], 0, [row("a1", "a", 0, 0.2, "failed")])
    case = result["windows"][0]["cases"][0]
    assert case["execution_states"] == {"failed": 1}
    assert case["median_latency_ns"] is None
    with pytest.raises(EvidenceError, match="duplicate"):
        summarize_windows(P, ["a"], 0, [row("a1", "a", 0, 0.2)] * 2)


def test_missing_or_invalid_terminal_timing_preserves_all_execution_states():
    requests = [
        row("ok", "a", 0, 0.1),
        row("failure", "a", 0.2, 0.3, "failed"),
        row("cancel", "a", 0.4, 0.5, "cancelled"),
        row("crash", "a", 0.6, 0.7, "invalid"),
        row("bad-clock", "a", 0.8, 0.7, "failed"),
    ]
    requests[2]["t_terminal_ns"] = None
    requests[3]["t_terminal_ns"] = None
    case = summarize_windows(P, ["a"], 0, requests)["windows"][0]["cases"][0]
    assert case["started"] == sum(case["execution_states"].values()) == 5
    assert case["execution_states"] == {"completed": 1, "failed": 2, "cancelled": 1, "invalid": 1}
    assert case["unfinished_or_invalid_timing"] == 3
    assert case["completed_in_send_cohort"] == 1
    assert case["median_latency_ns"] == 100_000_000
    assert case["failed"] == 2
    assert case["valid_executed"] == 3
