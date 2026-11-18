import pytest

from inferyard.analysis.duration_windows import summarize_windows
from inferyard.errors import EvidenceError

P = {"window_seconds": 1, "duration_seconds": 2.5, "min_completed_per_case_per_window": 1}


def row(key, case, start, end, state="completed"):
    return {
        "request_id": key,
        "case_id": case,
        "t_send_ns": int(start * 1e9),
        "t_terminal_ns": int(end * 1e9),
        "execution_state": state,
    }


def test_missing_first_window_never_borrows_later_baseline():
    result = summarize_windows(P, ["a"], 0, [row("a1", "a", 1, 1.2)])
    second = result["windows"][1]["cases"][0]
    assert second["median_latency_ns"] == 200_000_000
    assert second["latency_drift_ratio"] is None
    assert second["missing_reason"] == "initial_window_baseline_unavailable"


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
