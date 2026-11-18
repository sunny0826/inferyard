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
