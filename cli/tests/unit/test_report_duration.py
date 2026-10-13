from inferyard.reporting.report_duration import duration_view


def test_duration_view_keeps_zero_missing_and_failed_denominators():
    windows, metrics = [], []
    for i, value in enumerate((0, None, 25)):
        windows.append(
            {
                "index": i,
                "start_ns": 100 + i * 1000000000,
                "end_ns": 100 + (i + 1) * 1000000000,
                "cases": [{"case_id": "a", "started": 4, "cross_boundary_completed": 1}],
            }
        )
        for statistic, number in (
            ("median_latency", value),
            ("relative_change_from_first_window", None),
            ("cohort_failure_rate", 0.25),
        ):
            metrics.append(
                {
                    "metric_id": "S01",
                    "source": f"events.jsonl:case=a:window={i}",
                    "statistic": statistic,
                    "value": number,
                    "missing_reason": "window_observation_incomplete" if number is None else None,
                    "sample_count": 4,
                    "numerator": 1 if statistic == "cohort_failure_rate" else None,
                    "denominator": 4 if statistic == "cohort_failure_rate" else None,
                }
            )
    summary = {
        "duration": {
            "window_completed": False,
            "reason": "interrupted",
            "start_ns": 100,
            "windows": windows,
        },
        "metric_observations": metrics,
    }
    view = duration_view(summary)
    rows = view["cases"][0]["rows"]
    assert [r["latency"]["value"] for r in rows] == [0, None, 25]
    assert [r["start_seconds"] for r in rows] == [0, 1, 2]
    assert rows[1]["latency"]["reason"] == "window_observation_incomplete"
    assert rows[2]["failure"]["denominator"] == 4
    assert view["cases"][0]["latency_max"] == 25
    metrics.append(metrics[0])
    assert duration_view(summary)["cases"][0]["rows"][0]["latency"]["value"] is None


def test_non_duration_and_never_started_windows():
    assert duration_view({}) is None
    view = duration_view(
        {"duration": {"window_completed": False, "reason": "window_not_started", "windows": []}}
    )
    assert view["cases"] == [] and not view["completed"]
