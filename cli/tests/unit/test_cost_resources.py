"""Reference buckets preserve the full-scan resource reduction semantics."""

from copy import deepcopy

import pytest

from inferyard.analysis import resource_metrics as resources
from tests.unit.test_resources import CONFIG, ROW, cpu_sample


@pytest.mark.parametrize(
    "fault", ["none", "missing", "source", "phase", "clock", "window", "reset"]
)
def test_index_matches_full_scan_without_copying_or_reordering(monkeypatch, fault):
    requests = [{**ROW, "request_id": f"r{i}"} for i in range(4)]
    samples = []
    for row in requests:
        for metric in (
            "service_rss",
            "system_mem_available",
            "service_cpu_ticks",
            "system_swap_in",
            "system_swap_out",
        ):
            for time, value in [(1.5, 200), (0.5, 100), (0.5, 101)]:
                sample = cpu_sample(time, value)
                sample.update(
                    request_id=row["request_id"], metric_name=metric, page_size_bytes=4096
                )
                samples.append(sample)
    if fault == "missing":
        samples[0].update(value=None, missing_reason="permission_denied")
    elif fault == "source":
        samples[0]["server_pid"] += 1
    elif fault == "phase":
        samples[0]["phase"] = "warmup"
    elif fault == "clock":
        samples[0]["clock_id"] = "other"
    elif fault == "window":
        samples[0]["read_finished_ns"] = 3_000_000_000
    elif fault == "reset":
        samples[6]["value"] = 0
    baseline = {
        **samples[0],
        "request_id": None,
        "phase": "baseline",
        "read_started_ns": 0,
        "read_finished_ns": 0,
        "value": 500,
    }
    samples.append(baseline)
    original = deepcopy(samples)
    indexed = resources.reduce_resources(requests, samples, CONFIG)
    select = resources.select_samples
    visited = []

    def full_scan(row, bucket, metric, endpoint):
        assert all(
            item is original_item
            for item, original_item in zip(
                bucket,
                [
                    s
                    for s in samples
                    if s["request_id"] == row["request_id"] and s["metric_name"] == metric
                ],
                strict=True,
            )
        )
        visited.extend(bucket)
        return select(row, samples, metric, endpoint)

    monkeypatch.setattr(resources, "select_samples", full_scan)
    assert resources.reduce_resources(requests, samples, CONFIG) == indexed
    assert len(visited) == len(samples) - 1
    assert samples == original
