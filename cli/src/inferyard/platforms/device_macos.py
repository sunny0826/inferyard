"""Bounded, read-only macOS device queries; persist only public hardware fields."""

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
            devices.append(
                {
                    "name": row["sppci_model"],
                    "cores": _number(row.get("sppci_cores")),
                    "metal_supported": supported,
                    "memory_kind": "shared_host" if unified_memory else "unmeasured",
                    "memory_total_bytes": None,
                    "memory_free_bytes": None,
                    "missing": {
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
    return {**values, "sources": sources, "missing": missing}
