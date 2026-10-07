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
        SimpleNamespace(snapshot=lambda *args: None),
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
    )  # invalidated identity cannot recover silently
    assert sampler.counters(77, 55)[1]["value"] == 3  # host source remains independently available


def test_permission_and_boot_changes_are_missing_not_zero(tmp_path, monkeypatch):
    sampler = collector(tmp_path)

    def denied(*args):
        raise PermissionError()

    monkeypatch.setattr("inferyard.platforms.resources_linux.read_cpu", denied)
    assert sampler.counters(77, 55)[0]["missing_reason"] == "permission_denied"
    (tmp_path / "sys/kernel/random/boot_id").write_text("boot-2")
    assert all(
        s["value"] is None and s["missing_reason"] == "source_changed"
        for s in sampler.counters(77, 55)
    )


def test_cpu_uses_observed_wall_interval_not_whole_request():
    samples = [cpu_sample(0.5, 0), cpu_sample(1.5, 100)]
    reduced, observations = build_resource_observations(
        RUN, "w1", [ROW], samples, CONFIG, EVIDENCE, complete=True
    )
    metrics = reduced["requests"][0]["metrics"]
    assert metrics["C04"]["value"] == 1
    assert metrics["C04"]["coverage"] == 0.5
    assert metrics["C05"]["value"] == 100  # not 50% from dividing by the two-second request
    full = next(m for m in observations if m["statistic"] == "full_request_cpu_seconds")
    assert full["value"] is None and full["missing_reason"] == "request_boundaries_not_covered"
    assert (full["sampled_start_ns"], full["sampled_end_ns"]) == (500_000_000, 1_500_000_000)
    for item in observations:
        validate_document("metric_observation", item)


def test_one_core_percent_can_exceed_one_hundred_and_full_boundaries_are_exact():
    row = {**ROW, "t_terminal_ns": 500_000_000}
    data = reduce_resources([row], [cpu_sample(0, 0), cpu_sample(0.5, 150)], CONFIG)
    assert data["requests"][0]["metrics"]["C05"]["value"] == 300
    assert data["requests"][0]["metrics"]["C04"]["coverage"] == 1


@pytest.mark.parametrize(
    "fault,reason",
    [
        ("reset", "counter_reset"),
        ("component_reset", "counter_reset"),
        ("pid", "source_changed"),
        ("scale", "counter_source_or_scale_changed"),
        ("boot", "counter_source_or_scale_changed"),
        ("missing", "permission_denied"),
        ("boundary", "insufficient_counter_samples"),
        ("phase", "insufficient_counter_samples"),
    ],
)
def test_bad_counter_pairs_never_form_plausible_cpu_values(fault, reason):
    samples = [cpu_sample(0.5, 100, 10), cpu_sample(1.5, 200, 20)]
    if fault == "reset":
        samples[1].update(value=1, raw_cpu={"user_ticks": 1, "system_ticks": 0})
    elif fault == "component_reset":
        samples[1].update(value=220, raw_cpu={"user_ticks": 90, "system_ticks": 130})
    elif fault == "pid":
        samples[1]["process_start_ticks"] = 56
    elif fault == "scale":
        samples[1]["clock_ticks_per_second"] = 250
    elif fault == "boot":
        samples[1]["boot_id"] = "boot-2"
    elif fault == "missing":
        samples[1].update(value=None, raw_cpu=None, missing_reason="permission_denied")
    elif fault == "boundary":
        samples[1]["read_finished_ns"] = 2_000_000_001
    elif fault == "phase":
        samples[1]["phase"] = "warmup"
    metric = counter_delta(ROW, samples, "service_cpu_ticks", ENDPOINT)
    assert metric["value"] is None and metric["reason"] == reason


def test_memory_extrema_exclude_warmup_residual_and_cross_boundary_and_rss_delta_can_be_negative():
    def memory(t, value, phase="formal"):
        sample = {
            k: v
            for k, v in cpu_sample(t, 0).items()
            if k
            not in (
                "collector",
                "boot_id",
                "clock_ticks_per_second",
                "page_size_bytes",
                "raw_cpu",
                "capture",
            )
        }
        sample.update(
            metric_name="service_rss",
            source="/proc/<pid>/status:VmRSS",
            value=value,
            unit="bytes",
            phase=phase,
        )
        return sample

    row = {**ROW, "t_send_ns": 1_000_000_000}
    baseline = memory(0.5, 10, "baseline")
    baseline["request_id"] = None
    samples = [
        baseline,
        memory(0.8, 99, "warmup"),
        memory(1.2, 8),
        memory(1.8, 99, "residual"),
        memory(2.1, 99),
    ]
    data = reduce_resources([row], samples, CONFIG)
    assert data["baseline_rss_bytes"] == 10
    assert data["requests"][0]["metrics"]["C02"]["value"] == 8
    assert data["requests"][0]["metrics"]["C03"]["value"] == -2


def test_swap_is_host_pages_and_bytes_with_real_page_scale():
    samples = [cpu_sample(0.5, 3), cpu_sample(1.5, 5)]
    for s in samples:
        s.update(
            metric_name="system_swap_in",
            source="/proc/vmstat:pswpin",
            unit="pages",
            server_pid=None,
            process_start_ticks=None,
            raw_cpu=None,
            clock_ticks_per_second=None,
            page_size_bytes=16384,
        )
        validate_document("sample", s)
    _, metrics = build_resource_observations(
        RUN, "w1", [ROW], samples, CONFIG, EVIDENCE, complete=True
    )
    assert next(m for m in metrics if m["statistic"] == "system_swap_in_pages")["value"] == 2
    item = next(m for m in metrics if m["statistic"] == "system_swap_in_bytes")
    assert item["value"] == 32768 and item["unit"] == "bytes" and item["layer"] == "host"


def test_failed_requests_keep_resources_but_cancelled_do_not():
    samples = [cpu_sample(0.5, 0), cpu_sample(1.5, 100)]
    for state, expected in [("failed", 1), ("cancelled", None), ("invalid", None)]:
        row = {**ROW, "execution_state": state}
        data = reduce_resources([row], deepcopy(samples), CONFIG)
        assert data["requests"][0]["metrics"]["C04"]["value"] == expected
