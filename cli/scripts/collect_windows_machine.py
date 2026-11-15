"""Read-only Windows preparation inventory and benchmark resource reference on D:."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import platform
import shutil
import subprocess
import sys
import time
from datetime import UTC, datetime
from pathlib import Path
from urllib.parse import urlsplit

if __package__:
    from .windows_machine_probe import (
        cpu_sets,
        decode_power,
        executable_command,
        memory_status,
        native_features,
        nvidia_capabilities,
        nvidia_inventory,
        parse_cpu_sets,
        power_status,
        read_processor_power_settings,
        runtime_inventory,
    )
    from .windows_machine_report import null_fields, render_report
else:
    # Direct execution and spec loading do not put this directory on sys.path.
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    # decode_power/parse_cpu_sets are re-exported for the machine inventory tests.
    from windows_machine_probe import (
        cpu_sets,
        decode_power,
        executable_command,
        memory_status,
        native_features,
        nvidia_capabilities,
        nvidia_inventory,
        parse_cpu_sets,
        power_status,
        read_processor_power_settings,
        runtime_inventory,
    )
    from windows_machine_report import null_fields, render_report

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
