"""Read-only Windows preparation inventory and benchmark resource reference on D:."""

from __future__ import annotations

import argparse
import csv
import ctypes
import hashlib
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
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import urlsplit
from uuid import UUID

ROOT = Path(__file__).resolve().parents[1]
CIM_SECTIONS = (
    "system",
    "os",
    "os_build",
    "processors",
    "memory_modules",
    "memory_arrays",
    "pagefiles",
    "bios",
    "motherboard",
    "video_controllers",
    "physical_disks",
    "disks",
    "volumes",
    "batteries",
    "network_links",
    "thermal_zones",
    "device_security",
    "secure_boot",
)
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
REFERENCES = {
    "cpu_sets": "https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-system_cpu_set_information",
    "commit_memory": "https://learn.microsoft.com/en-us/windows/win32/api/psapi/ns-psapi-performance_information",
    "power": "https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-system_power_status",
    "gpu_wmi_limits": "https://learn.microsoft.com/en-us/windows/win32/cimwin32prov/win32-videocontroller",
    "feature_flags": "https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-isprocessorfeaturepresent",
}


def utc():
    return datetime.now(UTC).isoformat()


def write_json(path, value):
    with path.open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False) + "\n")


def observe(source, read):
    started = utc()
    try:
        data = read()
        result = {"status": "empty" if data == [] else "ok", "data": data, "missing_reason": None}
    except Exception as exc:
        # Each optional provider fails independently; incomplete evidence remains explicit.
        result = {"status": "unavailable", "data": None, "missing_reason": type(exc).__name__}
    return {"source": source, "started_utc": started, "finished_utc": utc(), **result}


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


def read_cim(out):
    raw_path = out / "cim.raw.json"
    status = observe(
        "PowerShell/CIM selected benchmark fields",
        lambda: executable_command(
            [
                "powershell.exe",
                "-NoProfile",
                "-NonInteractive",
                "-ExecutionPolicy",
                "Bypass",
                "-File",
                str(ROOT / "scripts/collect_windows_cim.ps1"),
                "-OutFile",
                str(raw_path),
            ],
            timeout=90,
        ),
    )
    try:
        result = json.loads(raw_path.read_text(encoding="utf-8")) if raw_path.exists() else {}
    except OSError, ValueError:
        result = {}
        status.update(status="unavailable", missing_reason="incomplete_provider_json")
    return status, result


def cim_sections(out):
    try:
        status, result = read_cim(out)
    except subprocess.TimeoutExpired:
        status = {"status": "unavailable", "data": None, "missing_reason": "provider_timeout"}
        raw = out / "cim.raw.json"
        try:
            result = json.loads(raw.read_text(encoding="utf-8")) if raw.exists() else {}
        except OSError, ValueError:
            result = {}
    for name in CIM_SECTIONS:
        result.setdefault(
            name,
            {
                "source": "PowerShell/CIM",
                "status": "unavailable",
                "data": None,
                "missing_reason": status["missing_reason"] or "provider_did_not_complete",
            },
        )
    return result


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
    for package in ("inferyard", "psutil", "httpx", "jinja2", "jsonschema", "ruff", "pytest"):
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


def dynamic_inventory():
    import psutil

    started = time.monotonic_ns()
    per_cpu = psutil.cpu_percent(interval=1.0, percpu=True)
    finished = time.monotonic_ns()
    processes, unreadable = [], 0
    for process in psutil.process_iter(["pid", "name", "memory_info"]):
        info = process.info
        if info["memory_info"] is None:
            unreadable += 1
            continue
        processes.append(
            {
                "pid": info["pid"],
                "name": info["name"],
                "working_set_bytes": info["memory_info"].rss,
            }
        )
    return {
        "cpu_busy_percent_per_logical_processor": per_cpu,
        "cpu_busy_percent_mean": sum(per_cpu) / len(per_cpu) if per_cpu else None,
        "sample_started_monotonic_ns": started,
        "sample_finished_monotonic_ns": finished,
        "collector_cpu_affinity": psutil.Process().cpu_affinity(),
        "physical_cores_psutil": psutil.cpu_count(logical=False),
        "logical_processors_psutil": psutil.cpu_count(),
        "top_working_sets": sorted(processes, key=lambda item: -item["working_set_bytes"])[:15],
        "working_set_scope": "per-process resident working set; shared pages overlap",
        "unreadable_process_memory_count": unreadable,
    }


def cpu_summary(entries):
    classes = {}
    for entry in entries:
        group = classes.setdefault(str(entry["efficiency_class"]), {"cores": set(), "logical": []})
        group["cores"].add((entry["group"], entry["core_index"]))
        group["logical"].append([entry["group"], entry["logical_processor_index"]])
    return {
        "physical_cores": len({(item["group"], item["core_index"]) for item in entries}),
        "logical_processors": len(entries),
        "processor_groups": sorted({item["group"] for item in entries}),
        "efficiency_classes": {
            key: {"physical_cores": len(value["cores"]), "logical_processors": value["logical"]}
            for key, value in classes.items()
        },
        "class_semantics": "higher EfficiencyClass indicates faster, less power-efficient cores",
        "entries": entries,
    }


def candidate_reference(config_path, sections):
    from inferyard.config.loader import load_config

    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    model = Path(config["model"]["local_path"])
    output = Path(config["output"]["root"])
    ancestor = output
    while not ancestor.exists() and ancestor != ancestor.parent:
        ancestor = ancestor.parent
    disk = shutil.disk_usage(ancestor)
    memory = sections["memory"]["data"]
    available = memory["PhysicalAvailable_bytes"] if memory else None
    floor = config["output"]["min_available_memory_bytes"]
    size = model.stat().st_size
    required = floor + size + 1024**3
    dynamic = sections["background_load"]["data"]
    affinity = dynamic["collector_cpu_affinity"] if dynamic else None
    power = sections["power_status"]["data"]
    ac = power["ac_online"] if power else None
    try:
        import psutil

        port = urlsplit(config["endpoint"]["url"]).port
        listening = any(
            item.status == psutil.CONN_LISTEN and item.laddr.port == port
            for item in psutil.net_connections(kind="tcp")
        )
    except OSError, ValueError, psutil.Error:
        listening = None
    return {
        "config_path": str(loaded.source),
        "config_file_sha256": hashlib.sha256(loaded.source.read_bytes()).hexdigest(),
        "configured_backend": config["engine"]["backend"],
        "configured_threads": config["conditions"]["threads"],
        "configured_threads_batch": config["conditions"]["threads_batch"],
        "configured_context": config["conditions"]["context_size"],
        "configured_slots": config["conditions"]["slots"],
        "threads_within_collector_affinity": (
            config["conditions"]["threads"] <= len(affinity) if affinity is not None else None
        ),
        "model_bytes": size,
        "available_physical_bytes_at_preparation": available,
        "loaded_model_memory_floor_bytes": floor,
        "startup_work_reserve_bytes": 1024**3,
        "required_before_model_start_bytes": required,
        "preparation_memory_sufficient": available >= required if available is not None else None,
        "memory_shortfall_bytes": max(0, required - available) if available is not None else None,
        "benchmark_output_root": str(output),
        "output_volume_free_bytes": disk.free,
        "output_disk_minimum_bytes": config["output"]["min_disk_bytes"],
        "preparation_disk_sufficient": disk.free >= config["output"]["min_disk_bytes"],
        "observed_ac_online": ac,
        "configured_ac_online": config["conditions"]["ac_online"],
        "ac_matches": ac == config["conditions"]["ac_online"] if ac is not None else None,
        "endpoint_port_listening": listening,
        "authority": "preparation reference only; recheck dynamic resources at launch",
        "budget_changed": False,
    }


def section_data(inventory, name):
    return inventory["sections"][name].get("data")


def gib(value):
    return f"{value / 1024**3:.2f} GiB" if type(value) in (int, float) else "未知"


def safe(value):
    return str(value).replace("|", "\\|").replace("\r", " ").replace("\n", " ")


def label(value):
    if value is None:
        return "未知"
    if type(value) is bool:
        return "是" if value else "否"
    return str(value)


def null_fields(value, prefix=""):
    result = []
    if value is None:
        return [prefix]
    if isinstance(value, dict):
        for key, item in value.items():
            result.extend(null_fields(item, f"{prefix}.{key}" if prefix else key))
    elif isinstance(value, list):
        for index, item in enumerate(value):
            result.extend(null_fields(item, f"{prefix}[{index}]"))
    return result


def render_report(inventory):
    cpu = section_data(inventory, "processors") or []
    system = section_data(inventory, "system") or []
    os_info = section_data(inventory, "os") or []
    memory = section_data(inventory, "memory") or {}
    topology = section_data(inventory, "cpu_topology") or {}
    power = section_data(inventory, "power_status") or {}
    load = section_data(inventory, "background_load") or {}
    os_label = " / ".join(
        str(item.get("Caption")) + " " + str(item.get("Version")) for item in os_info
    )
    lines = [
        "# Windows benchmark 准备阶段机器清单",
        "",
        f"采集时间：{inventory['started_utc']} 至 {inventory['finished_utc']}。",
        "这是准备阶段的静态信息和即时观测，动态资源会在启动前重新检查。",
        "",
        "| 项目 | 观测 |",
        "| --- | --- |",
        f"| 设备 | {safe(' / '.join(str(item.get('Model')) for item in system))} |",
        f"| 系统 | {safe(os_label)} |",
        f"| CPU | {safe(' / '.join(str(item.get('Name')) for item in cpu))} |",
        f"| CPU 拓扑 | {topology.get('physical_cores', '未知')} 物理核 / "
        f"{topology.get('logical_processors', '未知')} 逻辑处理器 |",
        f"| 系统可见物理内存 | {gib(memory.get('PhysicalTotal_bytes'))} |",
        f"| 当前可用物理内存 | {gib(memory.get('PhysicalAvailable_bytes'))} |",
        f"| 当前提交量 / 提交上限 | {gib(memory.get('CommitTotal_bytes'))} / "
        f"{gib(memory.get('CommitLimit_bytes'))} |",
        f"| 接通电源 / 电池存在 | {label(power.get('ac_online'))} / "
        f"{label(power.get('battery_present'))} |",
        f"| CPU 忙碌比例（1 秒采样） | {label(load.get('cpu_busy_percent_mean'))}% |",
        "",
        "## CPU 与内存",
        "",
    ]
    for kind, group in topology.get("efficiency_classes", {}).items():
        lines.append(
            f"- EfficiencyClass {kind}：{group['physical_cores']} 核，"
            f"逻辑处理器 {group['logical_processors']}。"
        )
    lines += ["", "类值越高表示更快、能效更低；保留 OS 类值，不猜测 P/E 标签。", ""]
    for item in section_data(inventory, "memory_modules") or []:
        lines.append(
            f"- {safe(item.get('DeviceLocator'))}：{gib(item.get('Capacity'))}，"
            f"配置速率 {item.get('ConfiguredClockSpeed')} MHz，{safe(item.get('PartNumber'))}。"
        )
    lines += ["", "## GPU、磁盘与后台负载", ""]
    for item in section_data(inventory, "video_controllers") or []:
        lines.append(
            f"- GPU：{safe(item.get('Name'))}，驱动 {safe(item.get('DriverVersion'))}。"
            "WMI AdapterRAM 仅保留原始值。"
        )
    nvidia = section_data(inventory, "nvidia") or {}
    for item in nvidia.get("gpus", []):
        lines.append(
            f"- NVIDIA：{safe(item['name'])}，总显存 {item['memory.total']} MiB，"
            f"可用 {item['memory.free']} MiB，温度 {item['temperature.gpu']} °C。"
        )
    plan = section_data(inventory, "power_plan") or {}
    lines += ["", f"当前电源方案：{safe(plan.get('stdout', '未知').strip())}", ""]
    for item in section_data(inventory, "physical_disks") or []:
        lines.append(
            f"- 物理盘：{safe(item.get('FriendlyName'))}，{safe(item.get('MediaType'))} / "
            f"{safe(item.get('BusType'))}，容量 {gib(item.get('Size'))}。"
        )
    for item in section_data(inventory, "volumes") or []:
        lines.append(
            f"- 卷 {safe(item['DeviceID'])}：{safe(item['FileSystem'])}，"
            f"总量 {gib(item['Size'])}，可用 {gib(item['FreeSpace'])}。"
        )
    lines += ["", "| 当前进程 | 工作集 |", "| --- | --- |"]
    for item in load.get("top_working_sets", []):
        lines.append(f"| {safe(item['name'])} ({item['pid']}) | {gib(item['working_set_bytes'])} |")
    lines += ["", "工作集含共享页，不能相加作为系统占用。", "", "## 当前候选配置参考", ""]
    candidate = section_data(inventory, "candidate_reference")
    if candidate:
        lines += [
            f"- 配置：`{candidate['config_path']}`。线程 {candidate['configured_threads']}，"
            f"上下文 {candidate['configured_context']}，槽位 {candidate['configured_slots']}。",
            f"- 加载后可用内存下限：{gib(candidate['loaded_model_memory_floor_bytes'])}；"
            f"模型大小：{gib(candidate['model_bytes'])}；工作空间预留：1 GiB。",
            f"- 启动前所需可用内存：{gib(candidate['required_before_model_start_bytes'])}；"
            f"本次观测满足：{label(candidate['preparation_memory_sufficient'])}；"
            f"缺口：{gib(candidate['memory_shortfall_bytes'])}。",
            f"- 输出磁盘预算满足：{label(candidate['preparation_disk_sufficient'])}；"
            f"电源条件满足：{label(candidate['ac_matches'])}；"
            f"目标端口已监听：{label(candidate['endpoint_port_listening'])}。",
            "- 原配置预算未调整；准备清单不授予性能或硬件资格。",
        ]
    else:
        lines.append("没有可用的候选配置参考，查看 JSON 中的缺失原因。")
    lines += [
        "",
        "## 采集覆盖与缺失",
        "",
        "| 类别 | 状态 | 来源 / 缺失原因 |",
        "| --- | --- | --- |",
    ]
    for name, section in inventory["sections"].items():
        lines.append(
            f"| {name} | {section['status']} | {safe(section['source'])} / "
            f"{safe(section.get('missing_reason') or '')} |"
        )
    lines += ["", "未获得数值的字段路径（见 JSON 的 null_fields）：", ""]
    lines += [f"- `{path}`" for path in inventory.get("null_fields", [])]
    lines += [
        "",
        *[f"- {note}" for note in inventory["limitations"]],
        "",
        "数值为 null 或提供者默认值（如 0）时，不推定对应能力成立。"
        "CIM 原始字段使用微软单位：内存条 Capacity 为 bytes，"
        "缓存和 OS 内存字段为 KiB，分页文件字段为 MiB；JSON 单位说明见 field_units。",
        "",
        "## API 参考",
        "",
        *[f"- [{key}]({url})" for key, url in REFERENCES.items()],
        "",
    ]
    return "\n".join(lines)


def collect_machine(out, *, config_path=None):
    if os.name != "nt":
        raise ValueError("native_windows_required")
    out = out.resolve()
    if out.drive.upper() != "D:" or not out.is_relative_to(ROOT):
        raise ValueError("machine_evidence_must_stay_in_D_workspace")
    out.mkdir(parents=True, exist_ok=False)
    started = utc()
    sections = cim_sections(out)
    sections.update(
        {
            "cpu_topology": observe("GetSystemCpuSetInformation", lambda: cpu_summary(cpu_sets())),
            "native_features": observe(
                "Windows kernel32 feature/firmware/clock APIs", native_features
            ),
            "power_status": observe("GetSystemPowerStatus", power_status),
            "power_plan": observe(
                "powercfg /getactivescheme",
                lambda: executable_command(
                    [
                        "powercfg.exe",
                        "/getactivescheme",
                    ]
                ),
            ),
            "processor_power_settings": observe(
                "powercfg /qh SCHEME_CURRENT SUB_PROCESSOR",
                lambda: executable_command(
                    [
                        "powercfg.exe",
                        "/qh",
                        "SCHEME_CURRENT",
                        "SUB_PROCESSOR",
                    ]
                ),
            ),
            "nvidia": observe("nvidia-smi read-only query", nvidia_inventory),
            "nvidia_capabilities": observe(
                "nvidia-smi read-only capability query", nvidia_capabilities
            ),
            "runtime": observe("Python metadata and D: preparation receipt", runtime_inventory),
            "background_load": observe(
                "psutil 1-second CPU sample and process working sets", dynamic_inventory
            ),
            # Take memory last: CIM/tool collection itself consumes temporary memory.
            "memory": observe("GetPerformanceInfo (pages multiplied by PageSize)", memory_status),
        }
    )
    sections["processor_power_indices"] = observe(
        "PowerReadACValueIndex/PowerReadDCValueIndex (read only)",
        lambda: read_processor_power_settings(
            sections["power_plan"]["data"]["stdout"],
            sections["processor_power_settings"]["data"]["stdout"],
        ),
    )
    if config_path is not None:
        sections["candidate_reference"] = observe(
            "Frozen candidate and observed resources",
            lambda: candidate_reference(config_path, sections),
        )
    else:
        sections["candidate_reference"] = {
            "source": "optional --config",
            "status": "unavailable",
            "data": None,
            "missing_reason": "candidate_not_supplied",
        }
    inventory = {
        "kind": "windows_benchmark_machine_inventory.v1",
        "schema_version": 3,
        "started_utc": started,
        "finished_utc": utc(),
        "platform": platform.platform(),
        "collection_stage": "preparation",
        "sections": sections,
        "field_units": {
            "Win32_PhysicalMemory.Capacity": "bytes",
            "Win32_PhysicalMemory.Speed/ConfiguredClockSpeed": "MHz (provider reported)",
            "Win32_Processor.*ClockSpeed": "MHz (provider reported; not sustained frequency)",
            "Win32_Processor.L2CacheSize/L3CacheSize": "KiB",
            "Win32_OperatingSystem.*MemorySize/FreePhysicalMemory/FreeVirtualMemory": "KiB",
            "Win32_PageFileUsage.AllocatedBaseSize/CurrentUsage/PeakUsage": "MiB",
            "Win32_LogicalDisk.Size/FreeSpace": "bytes",
            "MSAcpi_ThermalZoneTemperature.*Temperature/*TripPoint": (
                "tenths Kelvin; ACPI zone, not CPU package"
            ),
        },
        "limitations": [
            "静态参数与即时负载分开记录；启动和正式运行必须重新采集动态值。",
            "WMI AdapterRAM 为 uint32 提供者值，不能用于确认大显存容量；"
            "NVIDIA 容量使用 nvidia-smi。",
            "ACPI 热区不能视为 CPU 封装温度；"
            "CPU 实时封装温度、热降频、能耗与 RAPL 未获得可靠传感器证据。",
            "分页文件占用和提交量不能替代运行区间换页 I/O 计数。",
            "准备阶段电源方案记录不映射为 Linux governor/EPP，不改变已有 Windows 性能受限状态。",
            "未安装可选驱动或监控工具；缺失或权限不足按 unavailable 留证。",
        ],
        "reference_only": True,
        "hardware_qualification_added": False,
        "model_requests_sent": 0,
        "collector_source_sha256": {
            source.name: hashlib.sha256(source.read_bytes()).hexdigest()
            for source in (Path(__file__), Path(__file__).with_name("collect_windows_cim.ps1"))
        },
        "null_fields": [
            path
            for name, section in sections.items()
            for path in null_fields(section.get("data"), name)
        ],
    }
    write_json(out / "machine.json", inventory)
    with (out / "machine.md").open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(render_report(inventory))
    hashes = {
        path.name: hashlib.sha256(path.read_bytes()).hexdigest()
        for path in out.iterdir()
        if path.is_file()
    }
    write_json(out / "manifest.json", {"kind": "machine_inventory_files.v1", "sha256": hashes})
    return {
        "machine_json": str(out / "machine.json"),
        "machine_report": str(out / "machine.md"),
        "manifest": str(out / "manifest.json"),
        "machine_json_sha256": hashes["machine.json"],
        "unavailable_sections": [
            name for name, section in sections.items() if section["status"] == "unavailable"
        ],
        "candidate_reference": sections["candidate_reference"],
        "model_requests_sent": 0,
        "hardware_qualification_added": False,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True, help="new inventory directory on D:")
    parser.add_argument(
        "--config", type=Path, help="candidate to assess; its budget stays unchanged"
    )
    args = parser.parse_args()
    print(json.dumps(collect_machine(args.out, config_path=args.config), ensure_ascii=False))


if __name__ == "__main__":
    main()
