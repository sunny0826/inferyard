"""Identity, missing data and window rules shared by unified resource observations."""

from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.environment import assess_environment
from inferyard.analysis.resource_metrics import reduce_resources, select_samples
from inferyard.analysis.ledger import read_trial
from inferyard.platforms.telemetry import read_rss, sample_memory
from inferyard.analysis.comparison_input import comparison_input
from tests.helpers import fixture_run


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
