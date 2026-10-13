from copy import deepcopy

from inferyard.analysis.duration_windows import summarize_windows
from inferyard.analysis.idle_rss import SOURCE, idle_rss_observations
from inferyard.analysis.stability_observations import latency_drift_observations
from tests.unit.test_observations import EVIDENCE, RUN
from tests.unit.test_resources import CONFIG, ENDPOINT, envelope


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


def idle_inputs():
    rows = [{"request_id": f"r{i}", "clock_id": "clock-1"} for i in range(2)]
    samples = [
        envelope(
            {
                "source": SOURCE,
                "metric_name": "service_rss",
                "unit": "bytes",
                "phase": "residual",
                "request_id": row["request_id"],
                "value": value,
                "missing_reason": None,
                **ENDPOINT,
                "read_started_ns": 100 + i,
                "read_finished_ns": 100 + i,
            }
        )
        for i, (row, value) in enumerate(zip(rows, (1000, 900), strict=True))
    ]
    events = [
        {
            "event_type": "idle_observed",
            "request_id": row["request_id"],
            "phase": "residual",
            "data": {"state": "idle"},
            "monotonic_ns": 90,
        }
        for row in rows
    ]
    return rows, samples, events


def test_idle_rss_negative_delta_and_missing_first_cycle_not_rebased():
    rows, samples, events = idle_inputs()
    summary, items = idle_rss_observations(
        RUN, "w1", ["c"], rows, samples, events, CONFIG, EVIDENCE, complete=True
    )
    assert summary["first_cycle_rss_bytes"] == 1000
    assert items[-1]["value"] == -100
    missing = deepcopy(samples)
    missing[0].update(value=None, missing_reason="permission_denied")
    summary, items = idle_rss_observations(
        RUN, "w1", ["c"], rows, missing, events, CONFIG, EVIDENCE, complete=True
    )
    assert summary["first_cycle_rss_bytes"] is None
    assert items[-1]["value"] is None
    assert items[-1]["missing_reason"] == "first_cycle_baseline_missing"


def test_idle_claim_requires_observed_idle_and_bound_process():
    rows, samples, events = idle_inputs()
    samples[0]["server_pid"] += 1
    events[1]["data"]["state"] = "busy"
    _, items = idle_rss_observations(
        RUN, "w1", ["c"], rows, samples, events, CONFIG, EVIDENCE, complete=True
    )
    assert all(item["value"] is None for item in items)
