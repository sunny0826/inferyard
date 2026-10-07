import copy
import hashlib
import importlib
import json
from pathlib import Path

import pytest


@pytest.fixture
def bridge(monkeypatch):
    monkeypatch.syspath_prepend(str(Path(__file__).resolve().parents[2] / "scripts"))
    return importlib.import_module("analyze_observer")


def measured(value, source="fixture"):
    return {
        "value": value,
        "reason": None if value is not None else "fixture_missing",
        "source": source,
    }


def records():
    base = {"schema_version": 3, "definition": "lab_observer.v1", "session_id": "a" * 32}
    target = {"pid": 42, "process_start_ticks": 123, "name": "fixture"}
    own = {**target, "pid": 43}
    header = {
        **base,
        "kind": "observer_header",
        "tool_version": "0.1.0",
        "tool_source_sha256": measured("a" * 64),
        "binary_sha256": measured("b" * 64),
        "started_utc": "2026-10-02T00:00:00Z",
        "clock": "session_monotonic_ns_not_benchmark_clock",
        "interval_ns": 1_000_000_000,
        "duration_ns": 2_000_000_000,
        "targets": [target],
        "observer_target": own,
        "benchmark_binding": None,
        "system": {
            "os": "linux",
            "arch": "amd64",
            "os_version": measured("fixture"),
            "cpu_model": measured("fixture"),
            "logical_cpus": 2,
            "memory_total_bytes": measured(16 * 1024**3),
            "graphics_devices": measured(None),
            "limitations": ["fixture_only"],
        },
    }
    samples = []
    for index in range(2):
        start = index * 1_000_000_000
        percent = measured(None if index == 0 else 1.0, "counter_delta_over_monotonic_interval")
        if index == 0:
            percent["reason"] = "first_observation"
        proc = {
            **target,
            "state": "running",
            "read_started_ns": start + 2,
            "read_finished_ns": start + 4,
            "cpu_total_ns": measured(index * 10_000_000),
            "rss_bytes": measured(8 * 1024**2),
            "cpu_percent_one_core": percent,
        }
        samples.append(
            {
                **base,
                "kind": "observer_sample",
                "seq": index + 1,
                "read_started_ns": start,
                "read_finished_ns": start + 100,
                "processes": [proc],
                "observer": {**copy.deepcopy(proc), **own},
                "host": {
                    "memory_available_bytes": measured(1024**3),
                    "disk_available_bytes": measured(10 * 1024**3),
                },
                "endpoint_health": {
                    "state": "not_observed",
                    "reason": "not_requested",
                    "status_code": None,
                    "pid_ownership_verified": False,
                    "checked_utc": None,
                    "cached": False,
                },
            }
        )
    end = {
        **base,
        "kind": "observer_end",
        "samples": 2,
        "elapsed_ns": 2_000_000_000,
        "skipped_intervals": 0,
        "stop_reason": "duration_reached",
        "performance_comparison_qualified": False,
    }
    return [header, *samples, end]


def save(tmp_path, values):
    path = tmp_path / "fixture.jsonl"
    path.write_text("".join(json.dumps(value) + "\n" for value in values))
    return path


def test_complete_summary_keeps_source_and_diagnostic_scope(bridge, tmp_path):
    path = save(tmp_path, records())
    result = bridge.analyze(path)
    assert result["input_sha256"] == hashlib.sha256(path.read_bytes()).hexdigest()
    assert result["process"]["mean_cpu_percent_one_core"] == 1.0
    assert result["observer"]["peak_sampled_rss_bytes"] == 8 * 1024**2
    assert result["samples"] == 2
    assert result["performance_comparison_qualified"] is False


@pytest.mark.parametrize(
    "case",
    [
        "schema",
        "definition",
        "seq",
        "session",
        "clock",
        "identity",
        "source",
        "counter",
        "missing_reason",
        "boolean",
        "negative",
        "end_count",
        "cancelled",
        "no_end",
        "qualification",
        "ownership",
    ],
)
def test_corrupt_or_incomplete_stream_refused(bridge, tmp_path, case):
    values = records()
    sample = values[2]
    if case == "schema":
        values[0]["schema_version"] = 2
    elif case == "definition":
        values[0]["definition"] = "different"
    elif case == "seq":
        sample["seq"] = 3
    elif case == "session":
        sample["session_id"] = "b" * 32
    elif case == "clock":
        sample["read_started_ns"] = 0
    elif case == "identity":
        sample["processes"][0]["process_start_ticks"] += 1
    elif case == "source":
        sample["processes"][0]["rss_bytes"]["source"] = "other"
    elif case == "counter":
        sample["processes"][0]["cpu_percent_one_core"]["value"] = 99.0
    elif case == "missing_reason":
        sample["host"]["memory_available_bytes"] = measured(None)
        sample["host"]["memory_available_bytes"]["reason"] = None
    elif case == "boolean":
        sample["processes"][0]["rss_bytes"]["value"] = True
    elif case == "negative":
        sample["processes"][0]["cpu_total_ns"]["value"] = -1
    elif case == "end_count":
        values[-1]["samples"] = 3
    elif case == "cancelled":
        values[-1]["stop_reason"] = "cancelled"
    elif case == "no_end":
        values.pop()
    elif case == "qualification":
        values[-1]["performance_comparison_qualified"] = True
    else:
        sample["endpoint_health"]["pid_ownership_verified"] = True
    with pytest.raises(bridge.ObserverError):
        bridge.analyze(save(tmp_path, values))


@pytest.mark.parametrize(
    "bad", [b'{"x":1,"x":2}\n', b'{"x":NaN}\n', b'{"x":1e999}\n', b'{"x":"\xff"}\n', b"{}", b"\n"]
)
def test_bad_json_and_truncated_record_refused(bridge, tmp_path, bad):
    path = tmp_path / "bad.jsonl"
    path.write_bytes(bad)
    with pytest.raises(bridge.ObserverError):
        bridge.analyze(path)


def test_missing_counters_are_unknown_not_zero(bridge, tmp_path):
    values = records()
    for item in values[1:3]:
        for process in (item["processes"][0], item["observer"]):
            process["cpu_total_ns"] = measured(None)
            process["rss_bytes"] = measured(None)
            process["cpu_percent_one_core"] = measured(
                None, "counter_delta_over_monotonic_interval"
            )
            process["cpu_percent_one_core"]["reason"] = "cpu_counter_unavailable"
    result = bridge.analyze(save(tmp_path, values))
    assert result["process"]["peak_sampled_rss_bytes"] is None
    assert result["process"]["mean_cpu_percent_one_core"] is None
    assert result["process"]["rss_missing_samples"] == 2


def test_cli_preserves_existing_output_and_input(bridge, tmp_path):
    path = save(tmp_path, records())
    raw = path.read_bytes()
    out = tmp_path / "summary.json"
    args = ["--log", str(path), "--out", str(out)]
    assert bridge.main(args) == 0
    original = out.read_bytes()
    assert bridge.main(args) == 4
    assert out.read_bytes() == original
    assert path.read_bytes() == raw


def test_bounded_stream_refuses_oversize_line(bridge, tmp_path, monkeypatch):
    path = save(tmp_path, records())
    monkeypatch.setattr(bridge, "MAX_LINE", 64)
    with pytest.raises(bridge.ObserverError):
        bridge.analyze(path)
