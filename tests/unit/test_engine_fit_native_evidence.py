"""Versioned native evidence must not reinterpret Linux fields or SMC units."""

import shutil
from copy import deepcopy

import pytest

from inferyard.config.engine_fit import digest
from inferyard.evidence.storage import EvidenceError
from inferyard.reporting.engine_fit import compare, seal_run, verify
from inferyard.reporting.engine_fit_validation import validate_run
from tests.unit.test_engine_fit_report import _data, _idle
from tests.unit.test_engine_fit_report import plan as _plan_fixture


@pytest.fixture
def native_data():
    plan = _plan_fixture.__wrapped__()
    plan["host"]["platform"] = "Darwin"
    plan["plan_id"] = digest({k: v for k, v in plan.items() if k != "plan_id"})
    run, rows = _data(plan)
    run["definition"] = "engine_fit_run.v2"
    run["binding"].update(
        listener_inode=None,
        listener_identity="macos:tcp:127.0.0.1:8000:pid:123",
        listener_source="lsof:TCP:LISTEN:pid",
        cwd_sha256="6" * 64,
    )
    run["resources"][0].update(
        temperature_samples=[
            {
                "collector": "macos-smc.v1",
                "server_pid": None,
                "process_start_ticks": None,
                "phase": "engine_fit",
                "request_id": None,
                "metric_name": "temperature",
                "source": "AppleSMC:Tp01",
                "unit": "celsius",
                "semantics": "smc_key_reported",
                "raw_value": 42.5,
                "value": 42.5,
                "missing_reason": None,
                "read_started_ns": 100,
                "read_finished_ns": 200,
            }
        ],
        temperature_missing_reason=None,
    )
    return plan, run, rows


def test_macos_v2_seals_and_reads_without_live_platform(tmp_path, native_data, monkeypatch):
    plan, run, rows = native_data
    monkeypatch.setattr("platform.system", lambda: "Windows")
    seal_run(tmp_path, plan, run, rows)
    assert verify(tmp_path)["run"] == run


def test_macos_v2_comparison_reads_copied_evidence_offline(tmp_path, native_data, monkeypatch):
    plan, run, rows = native_data
    monkeypatch.setattr("platform.system", lambda: "Windows")
    paths = []
    for engine in ("vllm", "sglang"):
        item = deepcopy(run)
        item.update(engine=engine, run_id="run-" + engine)
        item["binding"]["model_binding"]["engine"] = engine
        item["service"].update(
            engine=engine,
            version_source="/version" if engine == "vllm" else "/get_server_info",
            idle_before=_idle(engine),
        )
        path = tmp_path / engine
        path.mkdir()
        seal_run(path, plan, item, rows)
        paths.append(path)
    out = tmp_path / "comparison"
    expected = compare(paths, out)
    for path in paths:
        shutil.rmtree(path)
    assert verify(out)["comparison"] == expected


def test_linux_v1_cannot_reinterpret_smc_temperature(native_data):
    plan, run, rows = native_data
    run["definition"] = "engine_fit_run.v1"
    for key in ("cwd_sha256", "listener_identity", "listener_source"):
        run["binding"].pop(key)
    run["binding"]["listener_inode"] = "56"
    with pytest.raises(EvidenceError, match="temperature_source"):
        validate_run(plan, run, rows)


@pytest.mark.parametrize(
    "key,value",
    [
        ("listener_inode", "1234"),
        ("listener_identity", "macos:tcp:127.0.0.1:8000:pid:999"),
        ("listener_source", "/proc/net/tcp"),
        ("cwd_sha256", "unverified"),
    ],
)
def test_macos_binding_rejects_forged_linux_or_other_pid(native_data, key, value):
    plan, run, rows = native_data
    run["binding"][key] = value
    with pytest.raises(EvidenceError):
        validate_run(plan, run, rows)


@pytest.mark.parametrize(
    "key,value",
    [
        ("collector", "linux-sensors.v2"),
        ("semantics", "thermal_zone_reported"),
        ("source", "/sys/class/thermal/thermal_zone0/temp"),
        ("raw_value", 42500),
        ("raw_value", True),
        ("value", float("nan")),
        ("value", float("inf")),
        ("value", 250),
        ("read_finished_ns", 99),
    ],
)
def test_macos_sensor_rejects_mixed_units_and_invalid_values(native_data, key, value):
    plan, run, rows = native_data
    run["resources"][0]["temperature_samples"][0][key] = value
    with pytest.raises(EvidenceError):
        validate_run(plan, run, rows)


def test_macos_missing_temperature_stays_null_with_reason(tmp_path, native_data):
    plan, run, rows = native_data
    resource = run["resources"][0]
    resource["temperature_samples"][0].update(
        raw_value=None, value=None, missing_reason="smc_source_unavailable"
    )
    resource["temperature_missing_reason"] = "no_verified_temperature_source"
    seal_run(tmp_path, plan, run, rows)
    assert verify(tmp_path)["run"]["resources"][0] == resource


def test_macos_fields_cannot_be_relabelled_v1(native_data):
    plan, run, rows = native_data
    run["definition"] = "engine_fit_run.v1"
    with pytest.raises(EvidenceError, match="binding"):
        validate_run(plan, run, rows)


def test_v2_cannot_relabel_linux_host(native_data):
    plan, run, rows = native_data
    plan["host"]["platform"] = "Linux"
    with pytest.raises(EvidenceError, match="run_platform"):
        validate_run(plan, run, rows)


def test_windows_live_rejected_before_loading_plan(monkeypatch):
    from types import SimpleNamespace

    from inferyard.platforms.identity import PreflightError
    from inferyard.runtime import engine_fit

    monkeypatch.setattr(engine_fit.platform, "system", lambda: "Windows")
    with pytest.raises(PreflightError, match="windows_engine_unavailable"):
        engine_fit.execute(SimpleNamespace(fit_engine="vllm"))


def test_macos_guard_stops_at_smc_temperature_without_linux_conversion(
    tmp_path, native_data, monkeypatch
):
    from inferyard.platforms import sensors_macos
    from inferyard.platforms.identity import PreflightError
    from inferyard.runtime import engine_fit

    plan, run, _ = native_data
    sample = run["resources"][0]
    sample["temperature_samples"][0].update(raw_value=86.5, value=86.5)

    class Sensors:
        sources = [{"metric_name": "temperature"}]

        def collect(self, phase, request_id):
            return sample["temperature_samples"]

    monkeypatch.setattr(engine_fit.platform, "system", lambda: "Darwin")
    monkeypatch.setattr(sensors_macos, "MacSensors", Sensors)
    monkeypatch.setattr(engine_fit, "check_service", lambda binding: None)
    monkeypatch.setattr(engine_fit, "resource_snapshot", lambda binding: sample)
    guard = engine_fit.Guard(run["binding"], plan, tmp_path)
    with pytest.raises(PreflightError, match="temperature_safety_threshold_reached"):
        guard.check("before_run")
    assert guard.last_sample["temperature_samples"][0]["value"] == 86.5
