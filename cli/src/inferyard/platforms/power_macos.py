"""Native macOS power policy; Linux governor/EPP remain explicitly unavailable."""

import re

SOURCE = "macos.iokit.power-policy.v1"
LEGACY_SOURCE = "macos.pmset.power-policy.v1"
SOURCES = frozenset({SOURCE, LEGACY_SOURCE})


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
        "source": LEGACY_SOURCE,
        "ac_online": ac_online,
        "low_power_mode": values["lowpowermode"],
        "power_mode": values.get("powermode", "unsupported"),
        "power_mode_supported": "powermode" in values,
    }


def native_policy(ac_online, low_power_mode, power_mode):
    """Build the current IOKit policy. power_mode None means the key is absent."""
    if (
        type(ac_online) is not bool
        or type(low_power_mode) is not int
        or low_power_mode not in (0, 1)
    ):
        return None
    if power_mode is None:
        mode, supported = "unsupported", False
    elif type(power_mode) is int and power_mode in (0, 1, 2):
        mode, supported = power_mode, True
    else:
        return None
    return {
        "source": SOURCE,
        "ac_online": ac_online,
        "low_power_mode": low_power_mode,
        "power_mode": mode,
        "power_mode_supported": supported,
    }


def read_native():
    """Read AC/battery and the active power profile once. Failure is (None, None)."""
    try:
        return _read_iokit()
    except AttributeError, OSError, ValueError:
        return None, None


def _cf():
    import ctypes

    core = ctypes.CDLL("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
    iokit = ctypes.CDLL("/System/Library/Frameworks/IOKit.framework/IOKit")
    core.CFStringGetCString.argtypes = [
        ctypes.c_void_p,
        ctypes.c_char_p,
        ctypes.c_long,
        ctypes.c_uint,
    ]
    core.CFStringGetCString.restype = ctypes.c_bool
    core.CFRelease.argtypes = [ctypes.c_void_p]
    core.CFRelease.restype = None
    core.CFGetTypeID.argtypes = [ctypes.c_void_p]
    core.CFGetTypeID.restype = ctypes.c_ulong
    core.CFNumberGetTypeID.restype = ctypes.c_ulong
    core.CFDictionaryGetTypeID.restype = ctypes.c_ulong
    core.CFNumberIsFloatType.argtypes = [ctypes.c_void_p]
    core.CFNumberIsFloatType.restype = ctypes.c_bool
    core.CFDictionaryGetValue.argtypes = [ctypes.c_void_p, ctypes.c_void_p]
    core.CFDictionaryGetValue.restype = ctypes.c_void_p
    core.CFStringCreateWithCString.argtypes = [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_uint]
    core.CFStringCreateWithCString.restype = ctypes.c_void_p
    core.CFNumberGetValue.argtypes = [ctypes.c_void_p, ctypes.c_int, ctypes.c_void_p]
    core.CFNumberGetValue.restype = ctypes.c_bool
    iokit.IOPSCopyPowerSourcesInfo.restype = ctypes.c_void_p
    iokit.IOPSGetProvidingPowerSourceType.argtypes = [ctypes.c_void_p]
    iokit.IOPSGetProvidingPowerSourceType.restype = ctypes.c_void_p
    iokit.IOPMCopyPMPreferences.restype = ctypes.c_void_p
    return core, iokit


def _text(core, ref):
    import ctypes

    if not ref:
        return None
    buffer = ctypes.create_string_buffer(256)
    if not core.CFStringGetCString(ref, buffer, 256, 0x08000100):
        return None
    return buffer.value.decode()


def _number(core, ref):
    import ctypes

    if not ref:
        return None
    if core.CFGetTypeID(ref) != core.CFNumberGetTypeID() or core.CFNumberIsFloatType(ref):
        raise ValueError("invalid_power_setting")
    value = ctypes.c_longlong()
    if not core.CFNumberGetValue(ref, 11, ctypes.byref(value)):
        raise ValueError("invalid_power_setting")
    return int(value.value)


def _section(core, preferences, name):
    if not preferences or core.CFGetTypeID(preferences) != core.CFDictionaryGetTypeID():
        raise ValueError("invalid_power_preferences")
    key = core.CFStringCreateWithCString(None, name.encode(), 0x08000100)
    try:
        if not key:
            raise ValueError("power_setting_key_unavailable")
        return core.CFDictionaryGetValue(preferences, key)
    finally:
        if key:
            core.CFRelease(key)


def _setting(core, section, name):
    return _number(core, _section(core, section, name))


def _read_iokit():
    core, iokit = _cf()
    blob = iokit.IOPSCopyPowerSourcesInfo()
    preferences = iokit.IOPMCopyPMPreferences()
    try:
        if not blob or not preferences:
            return None, None
        kind = _text(core, iokit.IOPSGetProvidingPowerSourceType(blob))
        ac = True if kind == "AC Power" else False if kind == "Battery Power" else None
        if ac is None:
            return None, None
        section = _section(core, preferences, "AC Power" if ac else "Battery Power")
        if not section:
            return ac, None
        low = _setting(core, section, "LowPowerMode")
        mode = _setting(core, section, "powermode")
        if mode is None:
            mode = _setting(core, section, "PowerMode")
        return ac, native_policy(ac, low, mode)
    finally:
        if blob:
            core.CFRelease(blob)
        if preferences:
            core.CFRelease(preferences)


def valid(value):
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
