"""Counter semantics and missing-data behavior for the read-only collector."""

import importlib.util
import json
import os
import subprocess
import sys
import time
from copy import deepcopy
from pathlib import Path

import pytest

SPEC = importlib.util.spec_from_file_location(
    "thermal_diagnostic", Path(__file__).parents[2] / "scripts/collect_thermal_diagnostic.py"
)
diag = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(diag)


def pair():
    before = {
        "monotonic": 10,
        "boot_id": "boot",
        "host_cpu": {"total": 1000, "idle": 800},
        "processes": {"42": {"ticks": 100, "start_ticks": 5}},
        "values": {"energy": "1000000", "throttle": "3"},
    }
    after = deepcopy(before)
    after.update(monotonic=12, host_cpu={"total": 1200, "idle": 900})
    after["processes"]["42"]["ticks"] = 200
    after["values"] = {"energy": "5000000", "throttle": "5"}
    channels = {"energy": {"kind": "energy_uj"}, "throttle": {"kind": "throttle_count"}}
    return before, after, channels


def test_interval_power_and_distinct_cpu_denominators():
    a, b, channels = pair()
    result = diag.derive(a, b, channels, 100)
    assert result["host_cpu_percent"] == 50
    assert result["process_cpu_percent_one_core"]["42"] == 50
    assert result["channels"]["energy"]["value"] == 2
    assert result["channels"]["throttle"]["value"] == 2


def test_pid_reuse_does_not_become_process_cpu():
    a, b, channels = pair()
    b["processes"]["42"]["start_ticks"] = 99
    assert diag.derive(a, b, channels, 100)["process_cpu_percent_one_core"]["42"] is None


@pytest.mark.parametrize("change", ["boot", "clock", "missing_boot"])
def test_invalid_counter_epoch_produces_no_deltas(change):
    a, b, channels = pair()
    if change == "boot":
        b["boot_id"] = "new"
    elif change == "clock":
        b["monotonic"] = a["monotonic"]
    else:
        a["boot_id"] = b["boot_id"] = {"unavailable": "PermissionError"}
    result = diag.derive(a, b, channels, 100)
    assert result["channels"] == {} and result["host_cpu_percent"] is None


@pytest.mark.parametrize("value", ["0", {"unavailable": "PermissionError"}, "nan"])
def test_missing_or_decreasing_energy_is_unknown(value):
    a, b, channels = pair()
    b["values"]["energy"] = value
    result = diag.derive(a, b, channels, 100)
    assert result["channels"]["energy"]["value"] is None
    assert result["channels"]["energy"]["reason"]


def test_proc_name_with_spaces_and_parentheses(tmp_path):
    p = tmp_path / "42"
    p.mkdir()
    fields = ["S"] + ["0"] * 19
    fields[11], fields[12], fields[19] = "20", "7", "123"
    (p / "stat").write_text("42 (name with ) parentheses) " + " ".join(fields))
    assert diag.process_stat(tmp_path, 42) == {"ticks": 27, "start_ticks": 123}
    assert "unavailable" in diag.process_stat(tmp_path, 99)


def test_discovery_freezes_sensor_identity_and_missing_samples(tmp_path):
    sys = tmp_path / "sys"
    zone = sys / "class/thermal/thermal_zone6"
    zone.mkdir(parents=True)
    (zone / "type").write_text("x86_pkg_temp")
    (zone / "temp").write_text("50000")
    channels = diag.discover(sys)
    assert channels[str(zone / "temp")]["label"] == "x86_pkg_temp"
    (zone / "temp").unlink()
    assert diag.snapshot(channels, tmp_path, [])["values"][str(zone / "temp")]["unavailable"]


def test_bounded_capture_preserves_missing_values_and_refuses_overwrite(tmp_path):
    out = tmp_path / "capture"
    result = diag.collect(out, seconds=0.1, interval=0.1, sys_root=tmp_path, proc=tmp_path)
    assert result["state"] == "completed"
    assert result["samples"] >= 2
    rows = [json.loads(s) for s in (out / "samples.jsonl").read_text().splitlines()]
    assert all(r["derived"]["host_cpu_percent"] is None for r in rows)
    assert not result["workload_started_or_stopped"]
    with pytest.raises(FileExistsError):
        diag.collect(out, seconds=0.1, interval=0.1, sys_root=tmp_path, proc=tmp_path)


@pytest.mark.parametrize("seconds,interval", [(float("nan"), 1), (1, 0), (3601, 1)])
def test_bad_bounds_create_no_output(tmp_path, seconds, interval):
    out = tmp_path / "invalid"
    with pytest.raises(ValueError):
        diag.collect(out, seconds=seconds, interval=interval)
    assert not out.exists()


@pytest.mark.skipif(sys.platform == "win32", reason="Linux collector and POSIX SIGTERM")
def test_term_records_terminal_without_stopping_observed_process(tmp_path):
    child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(30)"])
    out = tmp_path / "interrupted"
    collector = subprocess.Popen(
        [
            sys.executable,
            str(Path(diag.__file__)),
            "--out",
            str(out),
            "--seconds",
            "30",
            "--pid",
            str(child.pid),
        ],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    try:
        deadline = time.monotonic() + 5
        while not (out / "samples.jsonl").exists() and time.monotonic() < deadline:
            time.sleep(0.02)
        assert (out / "samples.jsonl").exists()
        collector.terminate()
        collector.wait(timeout=5)
        assert json.loads((out / "terminal.json").read_text())["state"] == "interrupted"
        assert child.poll() is None
    finally:
        for process in (collector, child):
            if process.poll() is None:
                process.kill()
            process.wait(timeout=5)


@pytest.mark.skipif(sys.platform != "linux", reason="Native Linux /proc process identity")
def test_recorded_pid_identity_is_initial_process(tmp_path):
    out = tmp_path / "self"
    diag.collect(out, seconds=0.1, interval=0.1, pids=[os.getpid()])
    meta = json.loads((out / "metadata.json").read_text())
    assert meta["process_identities"][str(os.getpid())]["start_ticks"] > 0
    assert meta["process_names"][str(os.getpid())]


def test_missing_tick_frequency_keeps_process_cpu_unknown():
    a, b, channels = pair()
    assert diag.derive(a, b, channels, None)["process_cpu_percent_one_core"]["42"] is None


@pytest.mark.skipif(sys.platform != "win32", reason="Windows public collector boundary")
def test_windows_linux_collector_rejects_before_creating_output(tmp_path):
    out = tmp_path / "unsupported"
    with pytest.raises(RuntimeError, match="linux_thermal_diagnostic_required"):
        diag.collect(out, seconds=0.1, interval=0.1)
    assert not out.exists()
