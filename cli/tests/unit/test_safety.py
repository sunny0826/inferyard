import asyncio

import pytest

from inferyard.errors import PreflightError
from inferyard.runtime.safety import ExternalCpu, SafetyGuard, monitor


class Sensors:
    sources = [{"metric_name": "temperature"}]

    def __init__(self, value):
        self.value = value

    def collect(self, *args):
        return [{"value": self.value}]

    def metadata(self):
        return {}


def test_thermal_samples_recorded_without_stopping_and_environment_changes_stop():
    policy = {
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
    sensors = Sensors(115)
    guard = SafetyGuard(policy, {"conditions": conditions}, lambda: env, sensors=sensors)
    guard.check()
    assert guard.last["temperature_samples"] == [{"value": 115}]
    sensors.value = None
    guard.check()
    assert guard.last["temperature_samples"] == [{"value": None}]
    sensors.value = 40
    env["ac_online"] = False
    with pytest.raises(PreflightError, match="environment_safety"):
        guard.check()
