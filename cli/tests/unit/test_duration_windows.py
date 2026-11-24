from inferyard.analysis.duration_windows import summarize_windows

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
