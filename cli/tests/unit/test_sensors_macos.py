import struct

import pytest

from inferyard.errors import ContractError, PreflightError
from inferyard.platforms.sensors_macos import MacSensors
from inferyard.platforms.smc_macos import SMCError
from inferyard.runtime.safety import SafetyGuard


class FakeSMC:
    entry_id = 17
    value = 50.25
    kind = "flt "

    def identity(self):
        return self.entry_id

    def keys(self):
        return ["Tp01", "Tg0C", "ABCD"]

    def info(self, key):
        return {"size": 4, "type": self.kind, "attributes": 1}

    def read(self, key):
        return self.info(key), struct.pack("<f", self.value)


def test_smc_temperature_still_enforces_90_degree_stop():

    source = FakeSMC()
    sensor = MacSensors(factory=lambda: source)
    policy = {
        "require_temperature": True,
        "max_temperature_celsius": 90,
        "check_environment": False,
        "max_external_cpu_percent": None,
    }
    guard = SafetyGuard(policy, {}, lambda: {}, sensors=sensor)
    guard.check()
    source.value = 90
    with pytest.raises(PreflightError, match="temperature_safety_threshold_reached"):
        guard.check()
    source.kind = "ui32"
    with pytest.raises(PreflightError, match="temperature_safety_source_unavailable"):
        guard.check()
