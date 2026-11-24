"""Sensor source changes must invalidate the affected window without cross-contamination."""

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
