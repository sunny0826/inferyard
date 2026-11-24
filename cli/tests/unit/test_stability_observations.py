from inferyard.analysis.duration_windows import summarize_windows
from inferyard.analysis.stability_observations import latency_drift_observations
from tests.unit.test_observations import EVIDENCE, RUN


def test_send_cohort_retains_slow_request_and_failure_denominator():
    protocol = {"duration_seconds": 2, "window_seconds": 1, "min_completed_per_case_per_window": 1}
    requests = [
        {
            "request_id": "a",
            "case_id": "c",
            "t_send_ns": 0,
            "t_terminal_ns": 200_000_000,
            "execution_state": "completed",
        },
        {
            "request_id": "b",
            "case_id": "c",
            "t_send_ns": 1_000_000_000,
            "t_terminal_ns": 2_500_000_000,
            "execution_state": "completed",
        },
        {
            "request_id": "f",
            "case_id": "c",
            "t_send_ns": 1_100_000_000,
            "t_terminal_ns": 2_800_000_000,
            "execution_state": "failed",
        },
    ]
    duration = {**summarize_windows(protocol, ["c"], 0, requests), "closed_ns": 3_000_000_000}
    items = latency_drift_observations(
        RUN, "w1", duration, {"c": {"category": "qa"}}, EVIDENCE, complete=True
    )
    second = {item["statistic"]: item for item in items if item["source"].endswith("window=1")}
    assert second["median_latency"]["value"] == 1500
    assert second["median_difference_from_first_window"]["value"] == 1300
    assert second["cohort_failure_rate"]["value"] == 0.5
    assert second["cohort_failure_rate"]["denominator"] == 2
    assert all(not item["comparison_eligible"] for item in items)
    duration["closed_ns"] = 1_500_000_000
    items = latency_drift_observations(
        RUN, "w1", duration, {"c": {"category": "qa"}}, EVIDENCE, complete=False
    )
    assert all(item["value"] is None for item in items if item["source"].endswith("window=1"))
