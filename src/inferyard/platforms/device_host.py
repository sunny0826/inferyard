"""Device-only host probes; live measurement identities retain their existing collectors."""

import os
from pathlib import Path


def _read(path):
    try:
        return Path(path).read_text().strip()
    except OSError, UnicodeError:
        return None


def _linux():
    cpu = _read("/proc/cpuinfo") or ""
    names = ("model name", "Hardware", "Processor")
    model = next(
        (
            line.split(":", 1)[1].strip()
            for name in names
            for line in cpu.splitlines()
            if ":" in line and line.split(":", 1)[0].strip() == name
        ),
        None,
    )
    memory = {}
    for line in (_read("/proc/meminfo") or "").splitlines():
        fields = line.split()
        if len(fields) == 3 and fields[0] in ("MemTotal:", "MemAvailable:"):
            try:
                number = int(fields[1])
                if number >= 0 and fields[2] == "kB":
                    memory[fields[0]] = number * 1024
            except ValueError:
                pass
    power = []
    for entry in sorted(Path("/sys/class/power_supply").glob("*")):
        if _read(entry / "type") in ("Mains", "USB", "USB_C"):
            if (online := _read(entry / "online")) in ("0", "1"):
                power.append(online == "1")
    return {
        "cpu_model": model,
        "logical_cpus": os.cpu_count(),
        "memory_total_bytes": memory.get("MemTotal:"),
        "mem_available_bytes": memory.get("MemAvailable:"),
        "ac_online": any(power) if power else None,
        "sources": {
            "cpu_model": "/proc/cpuinfo",
            "logical_cpus": "os.cpu_count",
            "memory_total_bytes": "/proc/meminfo:MemTotal",
            "memory_available_bytes": "/proc/meminfo:MemAvailable",
            "ac_online": "/sys/class/power_supply",
        },
    }


def _windows():
    import winreg

    import psutil

    values, missing = {"logical_cpus": os.cpu_count()}, {}
    try:
        with winreg.OpenKey(
            winreg.HKEY_LOCAL_MACHINE, r"HARDWARE\DESCRIPTION\System\CentralProcessor\0"
        ) as key:
            values["cpu_model"] = winreg.QueryValueEx(key, "ProcessorNameString")[0].strip()
    except OSError:
        missing["cpu_model"] = "registry_unavailable"
    try:
        memory = psutil.virtual_memory()
        values.update(memory_total_bytes=memory.total, mem_available_bytes=memory.available)
    except OSError, psutil.Error:
        missing.update(memory_total_bytes="query_failed", memory_available_bytes="query_failed")
    try:
        battery = psutil.sensors_battery()
        values["ac_online"] = battery.power_plugged if battery else None
    except OSError, psutil.Error:
        missing["ac_online"] = "query_failed"
    return {
        **values,
        "sources": {
            "cpu_model": "Windows_registry:ProcessorNameString",
            "logical_cpus": "os.cpu_count",
            "memory_total_bytes": "psutil.virtual_memory",
            "memory_available_bytes": "psutil.virtual_memory",
            "ac_online": "psutil.sensors_battery",
        },
        "missing": missing,
    }


def snapshot(system):
    if system == "Darwin":
        from inferyard.platforms.device_macos import snapshot as macos_snapshot

        return macos_snapshot()
    if system == "Windows":
        return _windows()
    if system == "Linux":
        return _linux()
    return {"sources": {}, "missing": {"platform": "unsupported_platform"}}
