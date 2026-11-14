"""Read-only Win32 observations for the Windows preparation inventory."""

import ctypes
import csv
import importlib.metadata
import io
import json
import math
import os
import platform
import re
import shutil
import struct
import subprocess
import sys
import time
from ctypes import wintypes
from pathlib import Path
from urllib.parse import urlsplit
from uuid import UUID

# Same checkout root as collect_windows_machine.py: this file lives in scripts/.
ROOT = Path(__file__).resolve().parents[1]
CACHE_KEYS = (
    "UV_CACHE_DIR",
    "UV_PROJECT_ENVIRONMENT",
    "UV_PYTHON_INSTALL_DIR",
    "UV_PYTHON_CACHE_DIR",
    "UV_TOOL_DIR",
    "UV_TOOL_BIN_DIR",
    "PIP_CACHE_DIR",
    "HF_HOME",
    "HF_HUB_CACHE",
    "HF_ASSETS_CACHE",
    "XDG_CACHE_HOME",
    "LLAMA_CACHE",
    "TEMP",
    "TMP",
)


def function(library, name, arguments, result=wintypes.BOOL):
    native = getattr(library, name)
    native.argtypes, native.restype = arguments, result
    return native


def check(success):
    if not success:
        raise ctypes.WinError(ctypes.get_last_error())


def parse_cpu_sets(raw):
    """Walk documented variable-sized entries; preserve group-relative indices."""
    result, offset = [], 0
    while offset < len(raw):
        if len(raw) - offset < 8:
            raise ValueError("truncated_cpu_set_header")
        size, kind = struct.unpack_from("<II", raw, offset)
        if size < 8 or offset + size > len(raw):
            raise ValueError("invalid_cpu_set_size")
        if kind == 0:
            if size < 32:
                raise ValueError("truncated_cpu_set")
            identifier, group, logical, core, cache, numa, efficiency, flags = struct.unpack_from(
                "<IHBBBBBB", raw, offset + 8
            )
            result.append(
                {
                    "id": identifier,
                    "group": group,
                    "logical_processor_index": logical,
                    "core_index": core,
                    "last_level_cache_index": cache,
                    "numa_node_index": numa,
                    "efficiency_class": efficiency,
                    "parked": bool(flags & 1),
                    "allocated": bool(flags & 2),
                    "allocated_to_current_process": bool(flags & 4),
                    "realtime": bool(flags & 8),
                }
            )
        offset += size
    return result


def cpu_sets():
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    get = function(
        kernel,
        "GetSystemCpuSetInformation",
        [
            ctypes.c_void_p,
            wintypes.ULONG,
            ctypes.POINTER(wintypes.ULONG),
            wintypes.HANDLE,
            wintypes.ULONG,
        ],
    )
    current = function(kernel, "GetCurrentProcess", [], wintypes.HANDLE)()
    size = wintypes.ULONG()
    get(None, 0, ctypes.byref(size), current, 0)
    if not size.value or size.value > 16 * 1024**2:
        raise RuntimeError("cpu_set_size_unavailable")
    buffer = ctypes.create_string_buffer(size.value)
    check(get(buffer, len(buffer), ctypes.byref(size), current, 0))
    return parse_cpu_sets(buffer.raw[: size.value])


class PerformanceInfo(ctypes.Structure):
    _fields_ = (
        [("cb", wintypes.DWORD)]
        + [
            (name, ctypes.c_size_t)
            for name in (
                "CommitTotal",
                "CommitLimit",
                "CommitPeak",
                "PhysicalTotal",
                "PhysicalAvailable",
                "SystemCache",
                "KernelTotal",
                "KernelPaged",
                "KernelNonpaged",
                "PageSize",
            )
        ]
        + [(name, wintypes.DWORD) for name in ("HandleCount", "ProcessCount", "ThreadCount")]
    )


def memory_status():
    get = function(
        ctypes.WinDLL("psapi", use_last_error=True),
        "GetPerformanceInfo",
        [
            ctypes.POINTER(PerformanceInfo),
            wintypes.DWORD,
        ],
    )
    info = PerformanceInfo()
    info.cb = ctypes.sizeof(info)
    check(get(ctypes.byref(info), info.cb))
    return {
        **{name + "_bytes": getattr(info, name) * info.PageSize for name, _ in info._fields_[1:10]},
        "page_size_bytes": info.PageSize,
        **{name: getattr(info, name) for name, _ in info._fields_[-3:]},
    }


class PowerStatus(ctypes.Structure):
    _fields_ = [
        (name, wintypes.BYTE)
        for name in (
            "ACLineStatus",
            "BatteryFlag",
            "BatteryLifePercent",
            "SystemStatusFlag",
        )
    ] + [(name, wintypes.DWORD) for name in ("BatteryLifeTime", "BatteryFullLifeTime")]


def decode_power(values):
    ac, flag, percent, saver, life, full = values
    return {
        "ac_online": bool(ac) if ac in (0, 1) else None,
        "battery_present": not bool(flag & 128) if flag != 255 else None,
        "battery_charging": bool(flag & 8) if flag != 255 else None,
        "battery_percent": percent if percent <= 100 else None,
        "battery_saver": bool(saver) if saver in (0, 1) else None,
        "remaining_seconds": life if life != 0xFFFFFFFF else None,
        "full_life_seconds": full if full != 0xFFFFFFFF else None,
        "raw": dict(zip((name for name, _ in PowerStatus._fields_), values, strict=True)),
    }


def power_status():
    get = function(
        ctypes.WinDLL("kernel32", use_last_error=True),
        "GetSystemPowerStatus",
        [
            ctypes.POINTER(PowerStatus),
        ],
    )
    value = PowerStatus()
    check(get(ctypes.byref(value)))
    return decode_power([getattr(value, name) for name, _ in value._fields_])


def native_features():
    kernel = ctypes.WinDLL("kernel32", use_last_error=True)
    present = function(kernel, "IsProcessorFeaturePresent", [wintypes.DWORD])
    firmware = function(kernel, "GetFirmwareType", [ctypes.POINTER(wintypes.DWORD)])
    kind = wintypes.DWORD()
    check(firmware(ctypes.byref(kind)))
    qpc = function(kernel, "QueryPerformanceFrequency", [ctypes.POINTER(ctypes.c_longlong)])
    frequency = ctypes.c_longlong()
    check(qpc(ctypes.byref(frequency)))
    return {
        "os_usable_processor_features": {
            name: bool(present(code))
            for name, code in {
                "sse2": 10,
                "sse3": 13,
                "ssse3": 36,
                "sse4_1": 37,
                "sse4_2": 38,
                "avx": 39,
                "avx2": 40,
                "avx512f": 41,
            }.items()
        },
        "feature_scope": "selected Windows flags; not exhaustive CPUID capabilities",
        "firmware_type": {1: "BIOS", 2: "UEFI"}.get(kind.value, "unknown"),
        "qpc_frequency_hz": frequency.value,
        "monotonic_clock": vars(time.get_clock_info("monotonic")),
    }


def executable_command(arguments, *, timeout=15):
    executable = shutil.which(arguments[0])
    if executable is None:
        raise FileNotFoundError("optional_tool_not_installed")
    # Native Windows tools already present on C: are read/executed, never installed there.
    process = subprocess.run(
        [executable, *arguments[1:]],
        capture_output=True,
        timeout=timeout,
        creationflags=subprocess.CREATE_NO_WINDOW,
    )
    # Keep text from localized tools intact without depending on PowerShell console encoding.
    import locale

    encoding = locale.getencoding()
    stdout = process.stdout.decode(encoding, errors="replace")
    if process.returncode:
        raise RuntimeError(f"tool_exit_{process.returncode}")
    return {"executable": executable, "stdout": stdout, "exit_code": process.returncode}


def nvidia_inventory():
    fields = (
        "index",
        "name",
        "driver_version",
        "memory.total",
        "memory.used",
        "memory.free",
        "temperature.gpu",
        "power.draw",
        "power.limit",
        "utilization.gpu",
        "pstate",
    )
    command = executable_command(
        [
            "nvidia-smi.exe",
            "--query-gpu=" + ",".join(fields),
            "--format=csv,noheader,nounits",
        ]
    )
    rows = []
    for row in csv.reader(io.StringIO(command["stdout"])):
        if len(row) != len(fields):
            raise ValueError("unexpected_nvidia_fields")
        values = {key: value.strip() for key, value in zip(fields, row, strict=True)}
        missing = {}
        for key in fields[3:]:
            if key == "pstate":
                continue
            try:
                values[key] = float(values[key])
                if not math.isfinite(values[key]):
                    raise ValueError("nonfinite_metric")
            except ValueError:
                missing[key] = "driver_metric_unavailable"
                values[key] = None
        values["missing_metrics"] = missing
        rows.append(values)
    return {"gpus": rows, "memory_unit": "MiB", "power_unit": "W", "temperature_unit": "C"}


def nvidia_capabilities():
    fields = (
        "compute_cap",
        "pcie.link.gen.max",
        "pcie.link.width.max",
        "clocks.max.sm",
        "clocks.max.memory",
    )
    command = executable_command(
        [
            "nvidia-smi.exe",
            "--query-gpu=" + ",".join(fields),
            "--format=csv,noheader,nounits",
        ]
    )
    rows = []
    for row in csv.reader(io.StringIO(command["stdout"])):
        if len(row) != len(fields):
            raise ValueError("unexpected_nvidia_capability_fields")
        values = {key: value.strip() for key, value in zip(fields, row, strict=True)}
        missing = {}
        for key, value in list(values.items()):
            if not re.fullmatch(r"\d+(?:\.\d+)?", value):
                values[key] = None
                missing[key] = "driver_metric_unavailable"
            elif key != "compute_cap":
                values[key] = float(value)
        values["missing_metrics"] = missing
        rows.append(values)
    return {
        "gpus_in_query_order": rows,
        "clock_unit": "MHz",
        "scope": "reported capabilities, not sustained benchmark performance",
    }


def read_processor_power_settings(plan_text, settings_text):
    pattern = r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}"
    match = re.search(pattern, plan_text)
    if match is None:
        raise ValueError("active_plan_guid_missing")
    plan = (ctypes.c_byte * 16).from_buffer_copy(UUID(match[0]).bytes_le)
    subgroup_id = "54533251-82be-4824-96c1-47b60b740d00"
    subgroup = (ctypes.c_byte * 16).from_buffer_copy(UUID(subgroup_id).bytes_le)
    library = ctypes.WinDLL("powrprof", use_last_error=True)
    reads = {
        key: function(
            library,
            name,
            [
                wintypes.HKEY,
                ctypes.c_void_p,
                ctypes.c_void_p,
                ctypes.c_void_p,
                ctypes.POINTER(wintypes.DWORD),
            ],
            wintypes.DWORD,
        )
        for key, name in {
            "ac_index": "PowerReadACValueIndex",
            "dc_index": "PowerReadDCValueIndex",
        }.items()
    }
    result = []
    for identifier in dict.fromkeys(re.findall(pattern, settings_text)):
        if identifier.lower() in (match[0].lower(), subgroup_id):
            continue
        setting = (ctypes.c_byte * 16).from_buffer_copy(UUID(identifier).bytes_le)
        item = {"setting_guid": identifier, "missing_indices": {}}
        for key, read in reads.items():
            index = wintypes.DWORD()
            error = read(None, plan, subgroup, setting, ctypes.byref(index))
            item[key] = index.value if error == 0 else None
            if error:
                item["missing_indices"][key] = f"win32_error_{error}"
        result.append(item)
    return {
        "active_plan_guid": match[0],
        "subgroup_guid": subgroup_id,
        "settings": result,
        "index_semantics": "Windows per-setting AC/DC index; raw powercfg describes each unit",
    }


def runtime_inventory():
    versions = {}
    for package in ("inferyard", "psutil", "httpx", "jsonschema", "ruff", "pytest"):
        try:
            versions[package] = importlib.metadata.version(package)
        except importlib.metadata.PackageNotFoundError:
            versions[package] = None
    paths = {key: os.environ.get(key) for key in CACHE_KEYS}
    managed = [sys.executable, sys._base_executable, sys.prefix, *paths.values()]
    receipt = ROOT / ".tools/downloads/windows-runtime.json"
    return {
        "python_version": platform.python_version(),
        "python_executable": sys.executable,
        "base_interpreter": sys._base_executable,
        "virtual_environment": sys.prefix,
        "packages": versions,
        "managed_paths": paths,
        "all_managed_paths_on_D": all(
            value and Path(value).drive.upper() == "D:" for value in managed
        ),
        "uv": executable_command([str(ROOT / ".tools/uv/uv.exe"), "--version"])["stdout"].strip(),
        "runtime_asset_receipt": json.loads(receipt.read_text()) if receipt.exists() else None,
        "receipt_scope": "previous verified preparation; inventory does not rehash model weights",
    }
