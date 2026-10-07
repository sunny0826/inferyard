import struct

import pytest

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


def test_key_identity_and_temperature_are_preserved_without_averaging():
    source = FakeSMC()
    sensor = MacSensors(factory=lambda: source)
    rows = sensor.collect("formal", "request")
    assert [r["source"] for r in rows] == ["AppleSMC:Tg0C", "AppleSMC:Tp01"]
    assert all(r["value"] == r["raw_value"] == 50.25 for r in rows)
    assert sensor.metadata()["sources"][0]["identity"]["registry_entry_id"] == 17
    source.entry_id = 18
    assert all(r["missing_reason"] == "source_changed" for r in sensor.collect("formal", "r"))
    source.entry_id = 17
    assert all(r["value"] is None for r in sensor.collect("formal", "r"))


@pytest.mark.parametrize("value", [float("nan"), float("inf"), -274, 201])
def test_nonfinite_or_invalid_temperature_is_missing(value):
    source = FakeSMC()
    sensor = MacSensors(factory=lambda: source)
    source.value = value
    assert all(
        r["value"] is None and r["missing_reason"] == "invalid_sensor_value"
        for r in sensor.collect("safety", None)
    )


def test_unknown_encoding_and_connection_failure_do_not_fabricate_zero():
    source = FakeSMC()
    source.kind = "ui32"
    assert MacSensors(factory=lambda: source).collect("safety", None) == []

    def unavailable():
        raise SMCError("smc_connection_unavailable")

    sensor = MacSensors(factory=unavailable)
    assert sensor.metadata()["discovery_issues"] == [{"reason": "smc_connection_unavailable"}]
    assert "temperature" in sensor.metadata()["missing_metrics"]


def test_smc_temperature_still_enforces_90_degree_stop():
    from inferyard.platforms.identity import PreflightError

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


def test_wire_contract_preserves_native_float_and_rejects_linux_float():
    from inferyard.contracts.validation import ContractError, validate_document
    from tests.unit.test_resources import envelope

    sample = envelope(MacSensors(factory=FakeSMC).collect("formal", "request-1")[0])
    validate_document("sample", sample)
    with pytest.raises(ContractError, match="conversion"):
        validate_document("sample", {**sample, "value": 50})
    linux = {
        **sample,
        "collector": "linux-sensors.v2",
        "source": "/sys/thermal/temp",
        "semantics": "thermal_zone_reported",
        "value": 0.05025,
    }
    with pytest.raises(ContractError, match="integer"):
        validate_document("sample", linux)
