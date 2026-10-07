"""Windows evidence stays versioned and offline; all observations here are synthetic."""

from copy import deepcopy

import pytest

from inferyard.config.engine_fit import digest
from inferyard.config.engine_fit_native_sources import (
    WINDOWS_LIMITATIONS,
    WINDOWS_SCOPE,
    WINDOWS_START_SOURCE,
)
from inferyard.evidence.storage import EvidenceError
from inferyard.reporting.engine_fit import seal_run, verify
from inferyard.reporting.engine_fit_validation import validate_run
from tests.unit.test_engine_fit_common_evidence import common_data
from tests.unit.test_engine_fit_memory_override import _data as memory_data
from tests.unit.test_engine_fit_temperature_override import _override_data as temperature_data


def windows_data(version=2):
    plan, run, rows = (
        memory_data("llama-cpp", both=True)
        if version == 4
        else temperature_data("llama-cpp")
        if version == 3
        else common_data("llama-cpp")
    )
    plan["host"]["platform"] = "Windows"
    plan["model"]["path"] = r"D:\models\model.gguf"
    plan["plan_id"] = digest({k: v for k, v in plan.items() if k != "plan_id"})
    run.update(definition="engine_fit_run.v6", platform="Windows", plan_id=plan["plan_id"])
    run["limitations"] = [
        item
        for item in run["limitations"]
        if item != "model_binding_is_startup_directory_not_observed_gpu_residency"
    ]
    run["limitations"].extend(WINDOWS_LIMITATIONS)
    run["binding"].update(
        listener_identity="windows:tcp:127.0.0.1:8000:pid:123",
        listener_source="GetExtendedTcpTable:owner_pid",
        process_start_source=WINDOWS_START_SOURCE,
        start_ticks=134_090_000_000_000_123,
    )
    run["binding"]["model_binding"]["path"] = plan["model"]["path"]
    resource = run["resources"][0]
    resource.update(
        scope=dict(WINDOWS_SCOPE),
        temperature_missing_reason=None,
        temperature_samples=[
            {
                "collector": "windows-nvidia-temperature.v1",
                "server_pid": None,
                "process_start_ticks": None,
                "phase": "engine_fit",
                "request_id": None,
                "metric_name": "temperature",
                "source": "nvidia-smi:gpu:0:temperature.gpu",
                "unit": "celsius",
                "semantics": "gpu_temperature_reported",
                "raw_value": 47.0,
                "value": 47.0,
                "missing_reason": None,
                "read_started_ns": 100,
                "read_finished_ns": 200,
            }
        ],
    )
    return plan, run, rows


@pytest.mark.parametrize("version", [2, 3, 4])
def test_windows_definition_seals_and_reads_without_live_windows(tmp_path, monkeypatch, version):
    data = windows_data(version)
    monkeypatch.setattr("platform.system", lambda: "Linux")
    seal_run(tmp_path, *data)
    assert verify(tmp_path)["run"] == data[1]
    html = (tmp_path / "report.html").read_text()
    if version >= 3:
        assert "本次已显式跳过温度停止" in html
    if version == 4:
        assert "本次已显式跳过内存停止" in html


@pytest.mark.parametrize(
    "key,value",
    [
        ("listener_inode", "123"),
        ("listener_source", "/proc/net/tcp"),
        ("listener_identity", "windows:tcp:127.0.0.1:8000:pid:999"),
        ("process_start_source", "psutil:create_time:seconds"),
        ("start_ticks", 134090000000000123.0),
    ],
)
def test_windows_binding_rejects_other_platforms_or_rounded_identity(key, value):
    plan, run, rows = windows_data()
    run["binding"][key] = value
    with pytest.raises(EvidenceError):
        validate_run(plan, run, rows)


@pytest.mark.parametrize(
    "device,inode",
    [(10_090_975_144_720_226_164, 11_258_999_068_579_525), (2**64 - 1, 2**128 - 1)],
)
def test_windows_unsigned_volume_and_full_file_id_seal_losslessly(tmp_path, device, inode):
    plan, run, rows = windows_data()
    run["binding"]["model_binding"].update(device=device, inode=inode)
    seal_run(tmp_path, plan, run, rows)
    assert verify(tmp_path)["run"]["binding"]["model_binding"] == run["binding"]["model_binding"]


@pytest.mark.parametrize(
    "key,value",
    [
        ("device", True),
        ("inode", True),
        ("device", -1),
        ("inode", -1),
        ("device", 2**64),
        ("inode", 2**128),
        ("device", float(2**63)),
    ],
)
def test_windows_file_identity_rejects_nonintegers_and_overflow(key, value):
    plan, run, rows = windows_data()
    run["binding"]["model_binding"][key] = value
    with pytest.raises(EvidenceError, match="model_binding_identity"):
        validate_run(plan, run, rows)


def test_old_definitions_keep_their_file_identity_integer_bound():
    plan, run, rows = common_data("llama-cpp")
    run["binding"]["model_binding"]["device"] = 10_090_975_144_720_226_164
    with pytest.raises(EvidenceError, match="model_binding_identity"):
        validate_run(plan, run, rows)


@pytest.mark.parametrize(
    "definition",
    [
        "engine_fit_run.v1",
        "engine_fit_run.v2",
        "engine_fit_run.v3",
        "engine_fit_run.v4",
        "engine_fit_run.v5",
    ],
)
def test_windows_evidence_cannot_be_relabelled_as_an_old_definition(definition):
    plan, run, rows = windows_data()
    run["definition"] = definition
    with pytest.raises(EvidenceError):
        validate_run(plan, run, rows)


@pytest.mark.parametrize(
    "key,value",
    [
        ("collector", "linux-sensors.v2"),
        ("semantics", "thermal_zone_reported"),
        ("source", "ACPI:thermal_zone"),
        ("raw_value", 47000),
        ("value", float("nan")),
        ("value", True),
        ("read_finished_ns", 99),
        ("server_pid", 123),
    ],
)
def test_windows_sensor_cannot_reinterpret_cpu_or_other_units(key, value):
    plan, run, rows = windows_data()
    run["resources"][0]["temperature_samples"][0][key] = value
    with pytest.raises(EvidenceError):
        validate_run(plan, run, rows)


def test_missing_gpu_reading_remains_null_and_offline_verifiable(tmp_path):
    plan, run, rows = windows_data()
    resource = run["resources"][0]
    resource["temperature_samples"][0].update(
        source="nvidia-smi:unavailable",
        raw_value=None,
        value=None,
        missing_reason="nvidia_temperature_tool_unavailable",
    )
    resource["temperature_missing_reason"] = "no_verified_temperature_source"
    seal_run(tmp_path, plan, run, rows)
    assert verify(tmp_path)["run"]["resources"][0] == resource


@pytest.mark.parametrize(
    "change",
    ["scope", "partial_tree", "zero_processes", "no_temperature", "limitations", "platform"],
)
def test_windows_resource_scope_and_limitations_are_required(change):
    plan, run, rows = windows_data()
    if change == "scope":
        run["resources"][0]["scope"]["rss"] = "/proc/PID/stat:rss_pages"
    elif change == "partial_tree":
        run["resources"][0]["process_count"] = None
        run["resources"][0]["missing_reasons"]["process_count"] = "unavailable"
    elif change == "zero_processes":
        run["resources"][0]["process_count"] = 0
    elif change == "no_temperature":
        run["resources"][0].pop("temperature_samples")
    elif change == "limitations":
        run["limitations"].remove(WINDOWS_LIMITATIONS[0])
    else:
        run["platform"] = "Darwin"
    with pytest.raises(EvidenceError):
        validate_run(plan, run, rows)


def test_windows_run_rejects_disabled_stop_reason():
    plan, run, rows = windows_data(4)
    run["stop_reason"] = "memory_safety_threshold_reached"
    with pytest.raises(EvidenceError, match="memory_override"):
        validate_run(plan, run, rows)


def test_windows_tampered_sealed_source_is_rejected(tmp_path):
    plan, run, rows = windows_data()
    seal_run(tmp_path, plan, run, rows)
    changed = deepcopy(run)
    changed["binding"]["process_start_source"] = "guessed"
    from inferyard.evidence.storage import json_bytes

    (tmp_path / "run.json").write_bytes(json_bytes(changed))
    with pytest.raises(EvidenceError, match="hash_mismatch"):
        verify(tmp_path)


@pytest.mark.parametrize(
    "kind,reason",
    [
        ("temperature", "temperature_safety_threshold_reached"),
        ("memory", "memory_safety_threshold_reached"),
        ("missing_memory", "system_memory_unavailable"),
    ],
)
def test_windows_guard_keeps_default_stops_and_explicit_override_reads(
    tmp_path,
    monkeypatch,
    kind,
    reason,
):
    from inferyard.platforms import engine_fit_windows_temperature as sensors
    from inferyard.platforms.identity import PreflightError
    from inferyard.runtime import engine_fit as runtime

    monkeypatch.setattr(runtime.platform, "system", lambda: "Windows")
    monkeypatch.setattr(runtime, "check_service", lambda _: None)
    monkeypatch.setattr(sensors.shutil, "which", lambda _: "synthetic-tool")
    monkeypatch.setattr(
        sensors, "_read_query", lambda _: "0, 90\n" if kind == "temperature" else "0,47\n"
    )
    plan, run, _ = windows_data()
    resource = deepcopy(run["resources"][0])
    resource["memory_available_bytes"] = (
        0 if kind == "memory" else None if kind == "missing_memory" else 2**30
    )
    if kind == "missing_memory":
        resource["missing_reasons"]["memory_available_bytes"] = "synthetic_memory_unavailable"
    monkeypatch.setattr(runtime, "resource_snapshot", lambda _: deepcopy(resource))
    guard = runtime.Guard(run["binding"], plan, tmp_path)
    with pytest.raises(PreflightError, match=reason):
        guard.check("before_run")
    covered = windows_data(4)[0]
    observed = runtime.Guard(run["binding"], covered, tmp_path).check("before_run")
    assert observed["memory_available_bytes"] == resource["memory_available_bytes"]
    assert observed["temperature_samples"][0]["value"] == (90 if kind == "temperature" else 47)
