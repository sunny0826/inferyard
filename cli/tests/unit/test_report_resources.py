from inferyard.reporting.report_resources import resource_view


def sample(seq, seconds, value, *, metric="service_rss", source="rss", reason=None):
    return dict(
        seq=seq,
        read_started_ns=int(seconds * 1e9),
        read_finished_ns=int(seconds * 1e9),
        metric_name=metric,
        source=source,
        value=value,
        missing_reason=reason,
        capture="periodic",
        clock_id="clock",
        phase="formal",
        server_pid=1,
        process_start_ticks=2,
        boot_id="boot",
        clock_ticks_per_second=100,
    )


def test_zero_missing_and_gaps_do_not_join_and_axes_are_aligned():
    rows = [
        sample(1, 0, 0),
        sample(2, 1, 1024**2),
        sample(3, 2, None, reason="unavailable"),
        sample(4, 3, 2 * 1024**2),
        sample(5, 8, 3 * 1024**2),
        sample(6, 4, 50, metric="temperature", source="thermal"),
    ]
    result = resource_view(rows, 1000)
    rss = next(c for c in result["charts"] if c["metric"] == "service_rss")
    thermal = next(c for c in result["charts"] if c["metric"] == "temperature")
    assert [p["value"] for p in rss["points"]] == [0, 1, None, 2, 3]
    assert len(rss["lines"]) == 3
    assert rss["missing_count"] == 1 and rss["gap_count"] == 1
    assert thermal["lines"][0]["single"][0] == 500
    assert result["duration_seconds"] == 8


def test_cpu_deltas_counter_reset_and_large_gap():
    rows = [
        sample(i, t, v, metric="service_cpu_ticks", source="cpu")
        for i, t, v in [(1, 0, 100), (2, 1, 250), (3, 2, 10), (4, 3, 110), (5, 8, 610)]
    ]
    result = resource_view(rows, 1000)["charts"][0]
    assert [p["value"] for p in result["points"]] == [None, 150, None, 100, None]
    assert result["reasons"] == ["baseline_required", "cpu_delta_unavailable", "sampling_gap"]


def test_empty_and_mixed_clocks_are_explicitly_unavailable():
    assert resource_view([], 500)["reason"] == "resource_samples_missing"
    rows = [sample(1, 0, 1), {**sample(2, 1, 2), "clock_id": "different"}]
    assert resource_view(rows, 500)["reason"] == "resource_clock_mismatch"
