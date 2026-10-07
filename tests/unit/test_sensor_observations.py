"""Sensor grouping must preserve request windows, source semantics and missing evidence."""

from copy import deepcopy

import pytest

from inferyard.analysis.observations import Observations
from inferyard.analysis.sensor_observations import add_sensor_observations
from tests.unit.test_observations import EVIDENCE, RUN


def request(identity="a", **changes):
    return {
        "request_id": identity,
        "category": "qa",
        "clock_id": "clock",
        "execution_state": "completed",
        "t_send_ns": 10,
        "t_terminal_ns": 100,
        **changes,
    }


def sample(metric="temperature", source="A", semantics="smc_key_reported", **changes):
    return {
        "metric_name": metric,
        "request_id": "a",
        "source": source,
        "semantics": semantics,
        "phase": "formal",
        "clock_id": "clock",
        "value": 42,
        "missing_reason": None,
        "read_started_ns": 20,
        "read_finished_ns": 30,
        **changes,
    }


def observe(requests, samples, *, complete=True):
    output = Observations(RUN, "workload", EVIDENCE, complete=complete)
    add_sensor_observations(output, requests, samples)
    return output.items


def test_sources_are_global_sorted_and_separate_by_metric_and_semantics():
    samples = [
        sample(source="B", request_id="b", value=80),
        sample("frequency", "A", "requested_or_driver_reported", value=3000),
        sample("frequency", "A", "hardware_reported", value=2000),
        sample(semantics="other_reported", value=10),
        sample(value=-4.5),
        sample("frequency", "A", "hardware_reported", value=1000),
        sample(source="Z", request_id=None, phase="baseline"),
    ]
    original = deepcopy(samples)
    rows = observe([request(), request("b", execution_state="failed")], samples)
    first, second = rows[:8], rows[8:]
    assert [r["metric_id"] for r in first] == ["C07"] * 4 + ["C08"] * 4
    assert [r["source"] for r in first] == ["A", "A", "B", "Z", "A", "A", "A", "A"]
    assert [r["value"] for r in first] == [10, -4.5, None, None, 2000, 1000, 3000, 3000]
    assert [r["statistic"] for r in first[4:]] == ["observed_max", "observed_min"] * 2
    assert first[0]["limitations"][0] == "other_reported"
    assert first[1]["limitations"][0] == "smc_key_reported"
    assert all(r["missing_reason"] == "no_sensor_samples_in_window" for r in first[2:4])
    assert second[2]["value"] == 80  # Failed requests still retain valid observations.
    assert all(r["request_id"] == "a" for r in first)
    assert all(r["request_id"] == "b" for r in second)
    assert all(not r["comparison_eligible"] for r in rows)
    assert samples == original


def test_window_exclusions_and_missing_reasons_are_not_lost_by_grouping():
    samples = [
        sample(value=90, read_started_ns=90, read_finished_ns=100),
        sample(value=-1, read_started_ns=10, read_finished_ns=11),
        sample(value=999, phase="warmup"),
        sample(value=999, phase="residual"),
        sample(value=999, clock_id="other"),
        sample(value=999, read_started_ns=9),
        sample(value=999, read_finished_ns=101),
        sample(value=None, missing_reason="permission_denied"),
        sample(value=999, request_id="b"),
        sample(value=999, request_id=None),
    ]
    row = observe([request()], samples)[0]
    assert row["value"] == 90
    assert row["sample_count"] == 2 and row["excluded"] == 6
    assert (row["sampled_start_ns"], row["sampled_end_ns"], row["coverage_ratio"]) == (10, 100, 1)
    assert row["missing_reason"] is None
    assert "permission_denied" in row["limitations"]
    assert "coverage_is_sample_span_not_continuous_observation" in row["limitations"]


def test_source_change_overrides_good_samples_without_contaminating_other_requests():
    samples = [
        sample(value=50),
        sample(value=None, missing_reason="source_changed"),
        sample(value=None, missing_reason="permission_denied"),
        sample(request_id="b", value=60),
        sample(source="B", value=70),
    ]
    rows = observe([request(), request("b")], samples)
    changed, independent = rows[0], rows[1]
    assert changed["value"] is None and changed["missing_reason"] == "source_changed"
    assert (changed["sample_count"], changed["excluded"]) == (1, 2)
    assert changed["sampled_start_ns"] is None and changed["coverage_ratio"] is None
    assert independent["value"] == 70
    assert rows[3]["value"] == 60


@pytest.mark.parametrize("state", ["cancelled", "invalid", "not_executed"])
def test_invalid_execution_keeps_counts_but_not_sensor_values(state):
    row = observe([request(execution_state=state)], [sample()], complete=False)[0]
    assert row["value"] is None and row["missing_reason"] == "request_not_validly_executed"
    assert row["sample_count"] == 1
    assert row["sampled_start_ns"] is None and row["coverage_ratio"] is None
    assert "incomplete_trial_subset_only" in row["limitations"]


@pytest.mark.parametrize("field", ["t_send_ns", "t_terminal_ns"])
def test_missing_request_boundary_excludes_samples(field):
    row = observe([request(**{field: None})], [sample()])[0]
    assert row["value"] is None and row["missing_reason"] == "no_sensor_samples_in_window"
    assert (row["sample_count"], row["excluded"]) == (0, 1)


def test_unavailable_metric_differs_from_source_without_samples_in_this_request():
    rows = observe([request(), request(None)], [sample(request_id="unselected")])
    assert len(rows) == 2
    assert [r["missing_reason"] for r in rows] == [
        "no_sensor_samples_in_window",
        "sensor_evidence_unavailable",
    ]
    assert observe([request(None)], []) == []


def test_whole_sample_timeline_is_not_rescanned_per_request_and_source():
    class CountedTimeline(list):
        visited = 0

        def __iter__(self):
            for item in super().__iter__():
                self.visited += 1
                yield item

    requests = [request(str(i)) for i in range(12)]
    samples = CountedTimeline(
        sample(source=str(source), request_id=row["request_id"])
        for row in requests
        for source in range(8)
    )
    rows = observe(requests, samples)
    assert len(rows) == 12 * 9
    assert samples.visited <= 2 * len(samples)
