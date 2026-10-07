"""Bounded synthetic query processes and explicit missing GPU temperatures."""

import subprocess
import sys

import pytest

from inferyard.platforms import engine_fit_windows_temperature as sensors
from inferyard.platforms.identity import PreflightError


@pytest.mark.parametrize(
    "output",
    [
        "",
        "0, nan\n",
        "0, inf\n",
        "0, 201\n",
        "0, -274\n",
        "0, 40, extra\n",
        "0,40\n0,41\n",
        "true,42\n",
    ],
)
def test_malformed_query_cannot_become_a_temperature(output):
    with pytest.raises(ValueError):
        sensors._values(output)


def test_gpu_values_are_celsius_and_missing_devices_stay_missing():
    assert sensors._values("0, 42\n1, [N/A]\n") == [
        ("nvidia-smi:gpu:0:temperature.gpu", 42.0, None),
        ("nvidia-smi:gpu:1:temperature.gpu", None, "nvidia_temperature_not_reported"),
    ]


def test_unavailable_tool_produces_null_source_not_fake_device(monkeypatch):
    monkeypatch.setattr(sensors.shutil, "which", lambda _: None)
    row = sensors.WindowsTemperature().collect("engine_fit", None)[0]
    assert row["source"] == "nvidia-smi:unavailable"
    assert row["raw_value"] is None and row["value"] is None
    assert row["missing_reason"] == "nvidia_temperature_tool_unavailable"
    assert row["read_finished_ns"] >= row["read_started_ns"]


@pytest.mark.parametrize(
    "script,reason",
    [
        ("import time; time.sleep(10)", "query_timeout"),
        (
            "import sys,time; sys.stdout.write('x'*65537); sys.stdout.flush(); time.sleep(10)",
            "output_too_large",
        ),
        ("raise SystemExit(7)", "query_failed"),
    ],
)
def test_private_query_is_bounded_and_reaped(monkeypatch, script, reason):
    real_popen, children = subprocess.Popen, []

    def spawn(command, **options):
        assert command[1:] == ["--query-gpu=index,temperature.gpu", "--format=csv,noheader,nounits"]
        process = real_popen([sys.executable, "-c", script], **options)
        children.append(process)
        return process

    monkeypatch.setattr(sensors.subprocess, "Popen", spawn)
    with pytest.raises(PreflightError, match=reason):
        sensors._read_query("synthetic-nvidia-smi")
    assert len(children) == 1 and children[0].poll() is not None


def test_complete_query_is_used_without_cpu_temperature_substitution(monkeypatch):
    monkeypatch.setattr(sensors.shutil, "which", lambda _: "synthetic-tool")
    monkeypatch.setattr(sensors, "_read_query", lambda _: "0, 47\n")
    row = sensors.WindowsTemperature().collect("engine_fit", None)[0]
    assert row["collector"] == "windows-nvidia-temperature.v1"
    assert row["semantics"] == "gpu_temperature_reported"
    assert row["raw_value"] == row["value"] == 47
    assert row["missing_reason"] is None
