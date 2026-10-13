"""Identity, missing data and window rules shared by unified resource observations."""

from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.environment import assess_environment
from inferyard.analysis.resource_metrics import reduce_resources, select_samples
from inferyard.evidence.ledger import read_trial
from inferyard.platforms.telemetry import read_rss, sample_memory
from inferyard.reporting.comparison_report import comparison_input
from tests.helpers import fixture_run


def test_pid_reuse_nulls_rss_but_not_system_memory(tmp_path, monkeypatch):
    import inferyard.platforms.telemetry as telemetry

    directory = tmp_path / "77"
    directory.mkdir()
    (directory / "status").write_text("VmRSS: 1024 kB\n")
    (tmp_path / "meminfo").write_text("MemAvailable: 2048 kB\n")
    ticks = iter([55, 56])
    monkeypatch.setattr(telemetry, "process_start_ticks", lambda *args: next(ticks))
    assert read_rss(77, 55, tmp_path) == (None, "source_changed")
    monkeypatch.setattr(telemetry, "process_start_ticks", lambda *args: 56)
    samples = sample_memory(77, 55, "formal", "request-1", tmp_path)
    assert samples[0]["value"] == 2048 * 1024
    assert samples[1]["value"] is None and samples[1]["missing_reason"] == "source_changed"


def test_missing_sensors_are_null_not_zero(tmp_path, monkeypatch):
    import inferyard.platforms.telemetry as telemetry

    monkeypatch.setattr(telemetry, "process_start_ticks", lambda *args: 55)
    samples = sample_memory(77, 55, "formal", "request-1", tmp_path)
    assert all(s["value"] is None and s["missing_reason"] for s in samples)


def test_swap_and_power_drift_preserve_quality_but_disqualify_environment(tmp_path):
    data = comparison_input(fixture_run(tmp_path, comparison_mode="model"))[0]
    start = deepcopy(data["environment_start"])
    start["swap_pages"] = {"pswpin": 100, "pswpout": 200}
    end = deepcopy(start)
    end["swap_pages"] = {"pswpin": 103, "pswpout": 207}
    end["ac_online"] = False
    assessment = assess_environment(
        start,
        end,
        [{"monotonic_ns": 1_500_000_000, "snapshot": end}],
        data["config"]["conditions"],
        data["requests"],
    )
    assert not assessment["stable_observed_environment"]
    assert {"environment_changed:ac_online", "system_swap_activity:pswpin"}.issubset(
        assessment["reasons"]
    )
    limited = deepcopy(data)
    limited["summary"]["measurement_context"]["environment"] = assessment
    limited["summary"]["measurement_context"]["environment_qualification"].update(
        eligible=False, reasons=assessment["reasons"]
    )
    proofs = [
        {
            "eligible": True,
            "reasons": [],
            "target_run_id": item["run"]["run_id"],
            "tolerance_ratio": 0.05,
        }
        for item in (data, limited)
    ]
    result = compare_trials(data, limited, performance_evidence=proofs)
    assert result["eligibility"]["quality"] and result["eligibility"]["completion"]
    assert not result["eligibility"]["performance"]
    assert "right:environment_not_qualified" in result["performance_analysis"]["blockers"]
    assert all(row["difference"] == 0 for row in result["quality_differences"] if row["eligible"])
    assert all(row["difference"] is None for row in result["performance_analysis"]["differences"])
    assert limited["summary"]["quality"] == data["summary"]["quality"]
    end["swap_pages"] = {}
    assessment = assess_environment(start, end, [], data["config"]["conditions"], [])
    assert {"swap_counter_unknown:pswpin", "swap_counter_unknown:pswpout"}.issubset(
        assessment["reasons"]
    )


def test_only_formal_valid_request_windows_count_and_short_requests_stay_missing(tmp_path):
    gib = 1024**3
    data = read_trial(
        fixture_run(tmp_path, states=["completed", "failed", "completed"], durations=[1, 1, 0.1])
    )
    first, failed, short = data["requests"]
    samples = []
    for phase, row, delta, system, rss in [
        ("baseline", None, 0, 12, 5),
        ("formal", first, 10, 10, 6),
        ("formal", first, 20, 9, 7),
        ("formal", failed, 10, 8, 8),
        ("warmup", first, 30, 1, 99),
        ("scoring", first, 40, 1, 99),
        ("residual", failed, 50, 1, 99),
    ]:
        for template, value in zip(data["samples"][:2], (system, rss), strict=True):
            at = row["t_send_ns"] + delta if row else 100
            samples.append(
                {
                    **template,
                    "phase": phase,
                    "request_id": row["request_id"] if row else None,
                    "value": value * gib,
                    "read_started_ns": at,
                    "read_finished_ns": at + 1,
                }
            )
    reduced = reduce_resources(data["requests"], samples, data["config"])
    metrics = {r["request_id"]: r["metrics"] for r in reduced["requests"]}
    assert reduced["baseline_rss_bytes"] == 5 * gib
    assert min(m["C01"]["value"] for m in metrics.values() if m["C01"]["value"]) == 8 * gib
    assert max(m["C02"]["value"] for m in metrics.values() if m["C02"]["value"]) == 8 * gib
    assert sum(m["C02"]["sample_count"] for m in metrics.values()) == 3
    assert metrics[first["request_id"]]["C02"]["excluded"] == 2
    assert metrics[failed["request_id"]]["C02"]["value"] == 8 * gib
    assert metrics[short["request_id"]]["C02"]["value"] is None
    assert metrics[short["request_id"]]["C02"]["reason"] == "no_resource_samples"


@pytest.mark.parametrize("change", ["start", "end", "phase", "clock"])
def test_outside_window_samples_are_counted_as_excluded(tmp_path, change):
    data = read_trial(fixture_run(tmp_path))
    row = data["requests"][0]
    sample = deepcopy(data["samples"][0])
    sample["value"] = 1
    if change == "start":
        sample["read_started_ns"] = row["t_send_ns"] - 1
    elif change == "end":
        sample["read_finished_ns"] = row["t_terminal_ns"] + 1
    elif change == "phase":
        sample["phase"] = "scoring"
    else:
        sample["clock_id"] = "another-clock"
    samples = [*data["samples"], sample]
    selected, excluded, reasons = select_samples(
        row, samples, "system_mem_available", data["config"]["endpoint"]
    )
    assert len(selected) == 1 and excluded == 1 and reasons == []
    reduced = reduce_resources(data["requests"], samples, data["config"])
    metric = reduced["requests"][0]["metrics"]["C01"]
    assert metric["value"] == 8 * 1024**3 and metric["excluded"] == 1


def test_missing_rss_does_not_erase_system_memory_observations(tmp_path):
    data = read_trial(fixture_run(tmp_path))
    for sample in data["samples"]:
        if sample["metric_name"] == "service_rss":
            sample.update(value=None, missing_reason="source_unavailable")
    reduced = reduce_resources(data["requests"], data["samples"], data["config"])
    for row in reduced["requests"]:
        assert row["metrics"]["C01"]["value"] == 8 * 1024**3
        rss = row["metrics"]["C02"]
        assert rss["value"] is None and rss["excluded"] == 1
        assert "source_unavailable" in rss["limitations"]
