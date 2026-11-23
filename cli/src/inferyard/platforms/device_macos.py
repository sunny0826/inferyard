"""Bounded, read-only macOS device queries; persist only public hardware fields."""

from pathlib import Path
import re
import subprocess

from inferyard.contracts.validation import strict_json_loads


def query(arguments, *, timeout=2):
    try:
        result = subprocess.run(arguments, capture_output=True, text=True, timeout=timeout)
        if result.returncode:
            return None, "query_failed"
        return result.stdout.strip(), None
    except FileNotFoundError:
        return None, "command_not_found"
    except PermissionError:
        return None, "permission_denied"
    except subprocess.TimeoutExpired:
        return None, "probe_timeout"
    except OSError, UnicodeError:
        return None, "query_failed"


def _number(value):
    if isinstance(value, bool) or not isinstance(value, (str, int)):
        return None
    try:
        number = int(value)
        return number if number > 0 else None
    except TypeError, ValueError, OverflowError:
        return None


def available_memory(text):
    """Estimate reclaimable memory without double counting overlapping page counters."""
    header = re.search(r"page size of (\d+) bytes", text or "")
    if not header or not (page_size := _number(header[1])):
        return None
    counts = []
    for label in ("free", "inactive", "speculative"):
        match = re.search(rf"^Pages {label}:\s*(\d+)\.?\s*$", text, re.MULTILINE)
        if not match:
            return None
        try:
            counts.append(int(match[1]))
        except ValueError:
            return None
    return sum(counts) * page_size


def _display_vendor(row):
    vendor_raw = str(row.get("spdisplays_vendor") or "").lower()
    model_raw = str(row.get("sppci_model") or "").lower()
    combined = f"{vendor_raw} {model_raw}"
    if "apple" in combined:
        return "apple"
    if "amd" in combined or "radeon" in combined or "0x1002" in combined:
        return "amd"
    if "intel" in combined or "0x8086" in combined:
        return "intel"
    if "nvidia" in combined or "geforce" in combined or "0x10de" in combined:
        return "nvidia"
    return None


def displays(text, *, unified_memory):
    try:
        data = strict_json_loads(text)
        rows = data["SPDisplaysDataType"]
        if not isinstance(rows, list):
            raise ValueError
        devices = []
        for row in rows:
            if not isinstance(row, dict) or not isinstance(row.get("sppci_model"), str):
                continue
            metal = row.get("spdisplays_mtlgpufamilysupport", row.get("spdisplays_metal"))
            supported = None
            if isinstance(metal, str) and metal:
                if "unsupported" in metal.lower() or "not supported" in metal.lower():
                    supported = False
                elif re.fullmatch(
                    r"spdisplays_metal(?:[1-9]\d*|_supported)", metal.lower()
                ) or metal.lower().startswith("supported"):
                    supported = True
            vendor = _display_vendor(row)
            devices.append(
                {
                    "vendor": vendor,
                    "name": row["sppci_model"],
                    "cores": _number(row.get("sppci_cores")),
                    "metal_supported": supported,
                    "memory_kind": "shared_host" if unified_memory else "unmeasured",
                    "memory_total_bytes": None,
                    "memory_free_bytes": None,
                    "missing": {
                        **({"vendor": "not_reported"} if vendor is None else {}),
                        **(
                            {"cores": "not_reported"}
                            if _number(row.get("sppci_cores")) is None
                            else {}
                        ),
                        "memory_total_bytes": "shared_host_memory"
                        if unified_memory
                        else "not_measured",
                        "memory_free_bytes": "not_measured",
                        **({"metal_supported": "not_reported"} if supported is None else {}),
                    },
                }
            )
        if not devices:
            raise ValueError
        return {"status": "observed", "source": "system_profiler", "devices": devices}
    except TypeError, ValueError, KeyError:
        return {"status": "unavailable", "reason": "invalid_output", "devices": []}


def xcode_clt_installed() -> tuple[bool | None, str | None]:
    """Read-only check whether Apple Xcode Command Line Tools are installed."""
    text, reason = query(["/usr/bin/xcode-select", "-p"])
    if reason is None:
        return (True, None) if bool(text) and Path(text).is_dir() else (False, "xcode_clt_missing")
    if reason == "query_failed":
        return False, "xcode_clt_missing"
    return None, reason


def snapshot():
    values, sources, missing = {}, {}, {}
    fields = {
        "cpu_model": "machdep.cpu.brand_string",
        "logical_cpus": "hw.logicalcpu",
        "physical_cpus": "hw.physicalcpu",
        "memory_total_bytes": "hw.memsize",
        "apple_silicon": "hw.optional.arm64",
    }
    for field, key in fields.items():
        text, reason = query(["/usr/sbin/sysctl", "-n", key])
        value = text if field == "cpu_model" else _number(text)
        if field == "apple_silicon":
            value = text == "1" if text in ("0", "1") else None
        values[field] = value or None if field != "apple_silicon" else value
        sources[field] = "sysctl:" + key
        if values[field] is None:
            missing[field] = reason or "invalid_output"
    text, reason = query(["/usr/bin/sw_vers", "-productVersion"])
    values["os_version"] = text or None
    sources["os_version"] = "sw_vers:productVersion"
    if values["os_version"] is None:
        missing["os_version"] = reason or "not_reported"
    text, reason = query(["/usr/bin/vm_stat"])
    available = available_memory(text)
    total = values["memory_total_bytes"]
    if available is not None and total is not None and available > total:
        available = None
    values["mem_available_bytes"] = available
    values["memory_available_is_estimate"] = True
    sources["memory_available_bytes"] = "vm_stat:free+inactive+speculative"
    if available is None:
        missing["memory_available_bytes"] = reason or "invalid_output"
    text, reason = query(["/usr/bin/pmset", "-g", "batt"])
    values["ac_online"] = (
        True
        if text and "'AC Power'" in text
        else False
        if text and "'Battery Power'" in text
        else None
    )
    sources["ac_online"] = "pmset:batt"
    if values["ac_online"] is None:
        missing["ac_online"] = reason or "not_reported"
    text, reason = query(
        [
            "/usr/sbin/system_profiler",
            "-json",
            "-detailLevel",
            "mini",
            "-timeout",
            "5",
            "SPDisplaysDataType",
        ],
        timeout=6,
    )
    values["apple_gpu"] = (
        {"status": "unavailable", "reason": reason, "devices": []}
        if reason
        else displays(text, unified_memory=values["apple_silicon"] is True)
    )
    clt_ok, clt_reason = xcode_clt_installed()
    values["xcode_clt_installed"] = clt_ok
    sources["xcode_clt_installed"] = "xcode-select:-p"
    if clt_ok is None and clt_reason:
        missing["xcode_clt_installed"] = clt_reason
    return {**values, "sources": sources, "missing": missing}
