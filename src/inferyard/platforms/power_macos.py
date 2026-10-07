"""Native macOS power policy; Linux governor/EPP remain explicitly unavailable."""

import re

SOURCE = "macos.pmset.power-policy.v1"


def parse(text, ac_online):
    if type(ac_online) is not bool or not isinstance(text, str):
        return None
    sections, active = {}, None
    for line in text.splitlines():
        if line in ("Battery Power:", "AC Power:"):
            active = line[:-1]
            if active in sections:
                return None
            sections[active] = {}
        elif active:
            match = re.fullmatch(r"\s+(lowpowermode|powermode)\s+([0-2])\s*", line)
            if match:
                key, value = match.groups()
                if key in sections[active]:
                    return None
                sections[active][key] = int(value)
            elif re.match(r"\s+(lowpowermode|powermode)\b", line):
                return None
    values = sections.get("AC Power" if ac_online else "Battery Power", {})
    if values.get("lowpowermode") not in (0, 1):
        return None
    return {
        "source": SOURCE,
        "ac_online": ac_online,
        "low_power_mode": values["lowpowermode"],
        "power_mode": values.get("powermode", "unsupported"),
        "power_mode_supported": "powermode" in values,
    }


def valid(value):
    return (
        isinstance(value, dict)
        and set(value)
        == {"source", "ac_online", "low_power_mode", "power_mode", "power_mode_supported"}
        and value["source"] == SOURCE
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


def assess(snapshots, conditions):
    reasons = set()
    frozen = conditions.get("macos_power_policy")
    if not valid(frozen):
        reasons.add("macos_power_policy_not_frozen")
    for value in snapshots:
        if not valid(value):
            reasons.add("macos_power_policy_unavailable")
        elif value != frozen:
            reasons.add("macos_power_policy_frozen_mismatch")
    if snapshots and any(value != snapshots[0] for value in snapshots[1:]):
        reasons.add("macos_power_policy_changed")
    return {
        "complete_and_stable": bool(snapshots) and not reasons,
        "reasons": sorted(reasons),
        "snapshot_count": len(snapshots),
    }
