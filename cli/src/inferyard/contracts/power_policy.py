"""Pure shape validation for the frozen macOS power-policy snapshot."""

SOURCE = "macos.iokit.power-policy.v1"
LEGACY_SOURCE = "macos.pmset.power-policy.v1"
SOURCES = frozenset({SOURCE, LEGACY_SOURCE})


def valid_power_policy(value):
    return (
        isinstance(value, dict)
        and set(value)
        == {"source", "ac_online", "low_power_mode", "power_mode", "power_mode_supported"}
        and value["source"] in SOURCES
        and type(value["ac_online"]) is bool
        and type(value["low_power_mode"]) is int
        and value["low_power_mode"] in (0, 1)
        and type(value["power_mode_supported"]) is bool
        and (
            type(value["power_mode"]) is int and value["power_mode"] in (0, 1, 2)
            if value["power_mode_supported"]
            else value["power_mode"] == "unsupported"
        )
    )
