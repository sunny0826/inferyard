"""Sensor records retain raw units and explicitly qualified frequency semantics."""

from copy import deepcopy

from inferyard.contracts.schemas_common import VERSION, enum, nullable
from inferyard.contracts.schemas_memory import MEMORY_SAMPLE

SENSOR_SAMPLE = deepcopy(MEMORY_SAMPLE)
SENSOR_SAMPLE["properties"].update(
    {
        "schema_version": VERSION,
        "collector": enum("linux-sensors.v2", "macos-smc.v1"),
        "metric_name": enum("temperature", "frequency"),
        "value": nullable({"type": "number"}),
        "raw_value": nullable({"type": "number"}),
        "unit": enum("celsius", "Hz"),
        "semantics": enum(
            "thermal_zone_reported",
            "hardware_reported",
            "requested_or_driver_reported",
            "smc_key_reported",
        ),
    }
)
SENSOR_SAMPLE["required"] = list(SENSOR_SAMPLE["properties"])


def validate_sensor_sample(data):
    from inferyard.contracts.validation import ContractError, _sample_invariants

    _sample_invariants(data)
    thermal = data["metric_name"] == "temperature"
    macos = data["collector"] == "macos-smc.v1"
    if macos:
        import re

        if (
            not thermal
            or data["semantics"] != "smc_key_reported"
            or not re.fullmatch(r"AppleSMC:T[pg][\x20-\x7e]{2}", data["source"])
        ):
            raise ContractError("sample.source", "macOS temperature source mismatch")
    if data["unit"] != ("celsius" if thermal else "Hz"):
        raise ContractError("sample.unit", "sensor unit mismatch")
    if not macos and (
        (data["semantics"] == "thermal_zone_reported") != thermal
        or data["semantics"] == "smc_key_reported"
    ):
        raise ContractError("sample.semantics", "sensor semantics mismatch")
    if data["server_pid"] is not None or data["process_start_ticks"] is not None:
        raise ContractError("sample.server_pid", "sensor is not process attribution")
    raw = data["raw_value"]
    if (raw is None) != (data["value"] is None):
        raise ContractError("sample.raw_value", "raw and converted values must both be missing")
    if raw is not None:
        if not macos and type(raw) is not int:
            raise ContractError("sample.raw_value", "Linux sensor raw value must be an integer")
        if (not -273.15 <= raw <= 200) if macos else (raw < -273150 if thermal else raw <= 0):
            raise ContractError("sample.raw_value", "invalid sensor range")
        if data["value"] != raw * (1 if macos else 0.001 if thermal else 1000):
            raise ContractError("sample.value", "sensor conversion mismatch")
