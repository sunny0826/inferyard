"""Sensor grouping must preserve request windows, source semantics and missing evidence."""

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


def test_global_sources_keep_request_metric_and_semantics_separate():
    samples = [
        sample(source="B", request_id="b", value=80),
        sample("frequency", "A", "requested_or_driver_reported", value=3000),
        sample("frequency", "A", "hardware_reported", value=2000),
        sample(semantics="other_reported", value=10),
        sample(value=-4.5),
        sample("frequency", "A", "hardware_reported", value=1000),
        sample(source="Z", request_id=None, phase="baseline"),
    ]
    rows = observe([request(), request("b", execution_state="failed")], samples)
    semantics = {
        "other_reported", "smc_key_reported", "hardware_reported", "requested_or_driver_reported"
    }
    actual = {
        (
            r["request_id"], r["metric_id"], r["source"],
            next(s for s in semantics if s in r["limitations"]), r["statistic"],
        ): (r["value"], r["missing_reason"])
        for r in rows
    }
    expected = {}
    for metric, source, meaning, statistic, a_value, b_value in [
        ("C07", "A", "other_reported", "observed_max", 10, None),
        ("C07", "A", "smc_key_reported", "observed_max", -4.5, None),
        ("C07", "B", "smc_key_reported", "observed_max", None, 80),
        ("C07", "Z", "smc_key_reported", "observed_max", None, None),
        ("C08", "A", "hardware_reported", "observed_max", 2000, None),
        ("C08", "A", "hardware_reported", "observed_min", 1000, None),
        ("C08", "A", "requested_or_driver_reported", "observed_max", 3000, None),
        ("C08", "A", "requested_or_driver_reported", "observed_min", 3000, None),
    ]:
        for request_id, value in (("a", a_value), ("b", b_value)):
            reason = "no_sensor_samples_in_window" if value is None else None
            expected[request_id, metric, source, meaning, statistic] = (value, reason)
    assert len(rows) == len(actual)  # Duplicate observations must not inflate the evidence.
    assert actual == expected
    assert all(not r["comparison_eligible"] for r in rows)


def test_source_change_overrides_good_samples_without_contaminating_other_requests():
    samples = [
        sample(value=50),
        sample(value=None, missing_reason="source_changed"),
        sample(value=None, missing_reason="permission_denied"),
        sample(request_id="b", value=60),
        sample(source="B", value=70),
    ]
    rows = {
        (r["request_id"], r["metric_id"], r["source"]): r
        for r in observe([request(), request("b")], samples)
    }
    changed, independent = rows["a", "C07", "A"], rows["a", "C07", "B"]
    assert changed["value"] is None and changed["missing_reason"] == "source_changed"
    assert (changed["sample_count"], changed["excluded"]) == (1, 2)
    assert changed["sampled_start_ns"] is None and changed["coverage_ratio"] is None
    assert independent["value"] == 70
    assert rows["b", "C07", "A"]["value"] == 60
