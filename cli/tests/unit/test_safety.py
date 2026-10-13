import asyncio

import pytest

from inferyard.platforms.identity import PreflightError
from inferyard.runtime.safety import ExternalCpu, SafetyGuard, monitor


class Sensors:
    sources = [{"metric_name": "temperature"}]

    def __init__(self, value):
        self.value = value

    def collect(self, *args):
        return [{"value": self.value}]

    def metadata(self):
        return {}


def test_thermal_threshold_missing_source_and_environment_changes():
    policy = {
        "max_temperature_celsius": 90,
        "require_temperature": True,
        "check_environment": True,
        "max_external_cpu_percent": None,
    }
    conditions = {
        "ac_online": True,
        "governor": "powersave",
        "profile": "balanced",
        "epp": "balance_performance",
    }
    env = dict(conditions)
    sensors = Sensors(89)
    guard = SafetyGuard(policy, {"conditions": conditions}, lambda: env, sensors=sensors)
    guard.check()
    sensors.value = 90
    with pytest.raises(PreflightError, match="temperature_safety_threshold"):
        guard.check()
    sensors.value = None
    with pytest.raises(PreflightError, match="source_unavailable"):
        guard.check()
    sensors.value = 40
    env["ac_online"] = False
    with pytest.raises(PreflightError, match="environment_safety"):
        guard.check()


def test_external_cpu_subtracts_service_and_rejects_counter_regression(tmp_path, monkeypatch):
    values = iter([100, 150, 150])
    monkeypatch.setattr(
        "inferyard.runtime.safety.read_cpu",
        lambda *a: {"user_ticks": next(values), "system_ticks": 0},
    )
    counter = ExternalCpu(1, 2, tmp_path)
    (tmp_path / "stat").write_text("cpu 200 0 0 800 0 0 0 0\n")
    assert counter.sample() is None
    (tmp_path / "stat").write_text("cpu 500 0 0 1500 0 0 0 0\n")
    assert counter.sample() == 25
    (tmp_path / "stat").write_text("cpu 1 0 0 1 0 0 0 0\n")
    with pytest.raises(PreflightError, match="counter_changed"):
        counter.sample()


def test_monitor_reports_stop_once_and_does_not_retry():
    calls, reasons = [], []

    def check():
        calls.append(True)
        raise PreflightError("temperature_safety_threshold_reached")

    asyncio.run(monitor(0.001, check, reasons.append))
    assert len(calls) == 1
    assert reasons == ["temperature_safety_threshold_reached"]


def test_frozen_no_turbo_condition_is_read_only_and_rechecked(tmp_path):
    path = tmp_path / "devices/system/cpu/intel_pstate/no_turbo"
    path.parent.mkdir(parents=True)
    path.write_text("1\n")
    policy = dict(
        max_temperature_celsius=90,
        require_temperature=True,
        check_environment=False,
        max_external_cpu_percent=None,
        intel_pstate_no_turbo=1,
    )
    guard = SafetyGuard(policy, {}, lambda: {}, sensors=Sensors(40), sys_root=tmp_path)
    guard.check()
    assert guard.last["intel_pstate_no_turbo"]["value"] == 1
    assert path.read_text() == "1\n"
    path.write_text("0\n")
    with pytest.raises(PreflightError, match="intel_pstate_safety_condition_changed"):
        guard.check(periodic=True)
    assert guard.last["intel_pstate_no_turbo"]["value"] == 0
    for value in ("", "true", "2", "1" * 30):
        path.write_text(value)
        with pytest.raises(PreflightError, match="intel_pstate_safety_source_unavailable"):
            guard.check()
        assert guard.last["intel_pstate_no_turbo"]["value"] is None
    path.unlink()
    with pytest.raises(PreflightError, match="intel_pstate_safety_source_unavailable"):
        guard.check()
