"""CPU/swap counter units, identity changes, request windows and coverage examples."""

from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.analysis.resource_metrics import counter_delta, reduce_resources
from inferyard.analysis.resource_observations import build_resource_observations
from inferyard.contracts.validation import validate_document
from inferyard.platforms.resources_linux import ResourceSampler, read_cpu
from tests.unit.test_observations import EVIDENCE, RUN

ENDPOINT = {"server_pid": 77, "process_start_ticks": 55}
CONFIG = {"endpoint": ENDPOINT, "telemetry": {"interval_ms": 500}}
ROW = {
    "request_id": "request-1",
    "case_id": "c1",
    "category": "qa",
    "clock_id": "clock-1",
    "execution_state": "completed",
    "t_send_ns": 0,
    "t_terminal_ns": 2_000_000_000,
}


def proc(tmp_path, user=10, system=20, ticks=55):
    directory = tmp_path / "77"
    directory.mkdir(exist_ok=True)
    fields = ["0"] * 50
    fields[0] = "S"
    fields[11:15] = [str(user), str(system), "9999", "9999"]
    fields[19] = str(ticks)
    (directory / "stat").write_text("77 (process ) with spaces) " + " ".join(fields))
    (tmp_path / "vmstat").write_text("pswpin 3\npswpout 4\n")
    boot = tmp_path / "sys/kernel/random/boot_id"
    boot.parent.mkdir(parents=True, exist_ok=True)
    boot.write_text("boot-1")


def collector(tmp_path, **kwargs):
    proc(tmp_path)
    return ResourceSampler(
        SimpleNamespace(observe=lambda *args, **kwargs: None),
        CONFIG,
        proc_root=tmp_path,
        ticks_per_second=100,
        page_size=4096,
        **kwargs,
    )


def envelope(sample, seq=1):
    return {
        **sample,
        "schema_version": 3,
        "experiment_id": "e1",
        "trial_id": "t1",
        "run_id": "r1",
        "clock_id": "clock-1",
        "seq": seq,
        "utc": "2026-09-30T00:00:00Z",
    }


def cpu_sample(t, user, system=0):
    return envelope(
        {
            "phase": "formal",
            "request_id": "request-1",
            "metric_name": "service_cpu_ticks",
            "value": user + system,
            "unit": "ticks",
            "source": "/proc/<pid>/stat:utime+stime",
            "read_started_ns": int(t * 1e9),
            "read_finished_ns": int(t * 1e9),
            **ENDPOINT,
            "missing_reason": None,
            "collector": "linux-resource.v2",
            "boot_id": "boot-1",
            "clock_ticks_per_second": 100,
            "page_size_bytes": None,
            "raw_cpu": {"user_ticks": user, "system_ticks": system},
            "capture": "periodic",
        }
    )


def test_linux_counters_bind_pid_and_do_not_include_child_cpu(tmp_path):
    sampler = collector(tmp_path)
    assert read_cpu(77, 55, tmp_path) == {"user_ticks": 10, "system_ticks": 20}
    sampler.set_phase("formal", "request-1")
    samples = sampler.counters(77, 55)
    assert [s["value"] for s in samples] == [30, 3, 4]
    assert [s["unit"] for s in samples] == ["ticks", "pages", "pages"]
    for sample in samples:
        validate_document("sample", envelope(sample))
    proc(tmp_path, ticks=56)
    assert sampler.counters(77, 55)[0]["missing_reason"] == "source_changed"
    proc(tmp_path)
    assert (
        sampler.counters(77, 55)[0]["value"] is None
    )
    assert sampler.counters(77, 55)[1]["value"] == 3


def test_cpu_uses_observed_wall_interval_not_whole_request():
    samples = [cpu_sample(0.5, 0), cpu_sample(1.5, 100)]
    reduced, observations = build_resource_observations(
        RUN, "w1", [ROW], samples, CONFIG, EVIDENCE, complete=True
    )
    metrics = reduced["requests"][0]["metrics"]
    assert metrics["C04"]["value"] == 1
    assert metrics["C04"]["coverage"] == 0.5
    assert metrics["C05"]["value"] == 100
    full = next(m for m in observations if m["statistic"] == "full_request_cpu_seconds")
    assert full["value"] is None and full["missing_reason"] == "request_boundaries_not_covered"
    assert (full["sampled_start_ns"], full["sampled_end_ns"]) == (500_000_000, 1_500_000_000)
    for item in observations:
        validate_document("metric_observation", item)
