"""S01 per-case window latency observations preserve send-time cohorts."""

from inferyard.analysis.observations import Observations


def latency_drift_observations(run, workload_id, duration, cases, evidence, *, complete):
    output = Observations(run, workload_id, evidence, complete=complete)
    closed = duration.get("closed_ns")
    windows = duration.get("windows", [])
    baseline_observed = bool(windows and closed is not None and closed >= windows[0]["end_ns"])
    for window in windows:
        observed = closed is not None and closed >= window["end_ns"]
        for group in window["cases"]:
            case_id = group["case_id"]
            enough = group["median_latency_ns"] is not None
            for statistic, key, unit, divisor in (
                ("median_latency", "median_latency_ns", "ms", 1e6),
                ("median_difference_from_first_window", "latency_difference_ns", "ms", 1e6),
                ("relative_change_from_first_window", "latency_drift_ratio", "ratio", 1),
            ):
                value = group[key]
                reason = group["missing_reason"]
                if not observed or (statistic != "median_latency" and not baseline_observed):
                    value, reason = None, "window_observation_incomplete"
                output.add(
                    "S01",
                    statistic,
                    value / divisor if value is not None else None,
                    category=cases[case_id]["category"],
                    source=f"events.jsonl:case={case_id}:window={window['index']}",
                    count=group["completed_in_send_cohort"],
                    excluded=group["started"] - group["completed_in_send_cohort"],
                    unit=unit,
                    reason=reason,
                    comparison=False,
                    interval=(window["start_ns"], window["end_ns"])
                    if observed and enough
                    else None,
                    limits=[
                        "send_time_cohort_full_terminal_latency",
                        "paired_by_case_not_pooled_composition",
                        "window_interval_is_send_cohort_not_terminal_cutoff",
                        "drift_is_not_causal_throttling_evidence",
                    ],
                )
            valid = group["valid_executed"]
            output.add(
                "S01",
                "cohort_failure_rate",
                group["failed"] / valid if valid and observed else None,
                category=cases[case_id]["category"],
                unit="ratio",
                source=f"events.jsonl:case={case_id}:window={window['index']}",
                count=valid,
                numerator=group["failed"] if observed else None,
                denominator=valid if observed else None,
                excluded=group["started"] - valid,
                reason="window_observation_incomplete" if not observed else "zero_denominator",
                comparison=False,
                limits=["failure_denominator_completed_plus_failed"],
            )
    return output.items
