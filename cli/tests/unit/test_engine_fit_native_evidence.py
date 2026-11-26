"""Versioned native evidence must not reinterpret Linux fields or SMC units."""

import pytest

from inferyard.contracts.json_digest import digest
from inferyard.errors import PreflightError
from tests.unit.test_engine_fit_report import _data
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


def test_macos_guard_records_smc_temperature_without_stopping_or_linux_conversion(
    tmp_path, native_data, monkeypatch
):
    from inferyard.platforms import sensors_macos
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
    recorded = guard.check("before_run")
    assert recorded["temperature_samples"][0]["value"] == 86.5
    assert guard.last_sample["temperature_samples"][0]["value"] == 86.5
