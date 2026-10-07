"""Read local hardware and GGUF candidates, then recommend an explicit execution mode."""

import csv
import io
import math
import os
import platform
import shutil
import subprocess
from datetime import UTC, datetime
from pathlib import Path

from inferyard.evidence.storage import atomic_bytes, json_bytes
from inferyard.platforms.device_host import snapshot as host_snapshot
from inferyard.platforms.gguf_metadata import read_metadata

GIB = 1024**3
GPU_RESERVE = GIB
GPU_QUERY = (
    "index,name,driver_version,memory.total,memory.free,utilization.gpu,temperature.gpu,power.draw"
)


def nvidia_snapshot():
    executable = shutil.which("nvidia-smi")
    if executable is None:
        return {"status": "unavailable", "reason": "nvidia_smi_not_found", "devices": []}
    try:
        result = subprocess.run(
            [executable, "--query-gpu=" + GPU_QUERY, "--format=csv,noheader,nounits"],
            capture_output=True,
            text=True,
            timeout=8,
            creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
        )
        if result.returncode:
            raise ValueError("nvidia_driver_query_failed")
        devices = []
        for row in csv.reader(io.StringIO(result.stdout)):
            if len(row) != 8:
                raise ValueError("invalid_nvidia_query")
            values = [cell.strip() for cell in row]

            missing = {}

            def number(cell, field, *, required=False, missing=missing):
                try:
                    value = float(cell)
                    if not math.isfinite(value) or (value < 0 and field != "temperature_c"):
                        raise ValueError
                    if field == "utilization_percent" and value > 100:
                        raise ValueError
                    return value
                except ValueError:
                    if required:
                        raise ValueError("invalid_nvidia_query") from None
                    missing[field] = (
                        "not_reported"
                        if "N/A" in cell or "Not Supported" in cell
                        else "invalid_output"
                    )
                    return None

            total = int(number(values[3], "memory_total_bytes", required=True) * 1024**2)
            free = int(number(values[4], "memory_free_bytes", required=True) * 1024**2)
            index = int(values[0])
            if index < 0 or not values[1] or total <= 0 or free > total:
                raise ValueError("invalid_nvidia_query")

            devices.append(
                {
                    "index": index,
                    "name": values[1],
                    "driver_version": values[2],
                    "memory_total_bytes": total,
                    "memory_free_bytes": free,
                    "utilization_percent": number(values[5], "utilization_percent"),
                    "temperature_c": number(values[6], "temperature_c"),
                    "power_w": number(values[7], "power_w"),
                    "missing": missing,
                }
            )
        if not devices:
            raise ValueError("no_nvidia_devices")
        return {"status": "observed", "source": "nvidia-smi", "devices": devices}
    except subprocess.TimeoutExpired:
        return {"status": "unavailable", "reason": "probe_timeout", "devices": []}
    except OSError, UnicodeError, ValueError, OverflowError:
        return {"status": "unavailable", "reason": "nvidia_driver_query_failed", "devices": []}


def hardware_snapshot(disk_path, environment=None):
    system = platform.system()
    env = environment if environment is not None else host_snapshot(system)
    sources = dict(env.get("sources", {}))
    missing = dict(env.get("missing", {}))
    disk_path = Path(disk_path).resolve()
    try:
        disk_free = shutil.disk_usage(disk_path).free
    except OSError:
        disk_free = None
        missing["disk_free_bytes"] = "disk_usage_unavailable"
    result = {
        "observed_at": datetime.now(UTC).isoformat(),
        "platform": system,
        "architecture": platform.machine(),
        "cpu_model": env.get("cpu_model"),
        "logical_cpus": env.get("logical_cpus") or os.cpu_count(),
        "physical_cpus": env.get("physical_cpus"),
        "memory_total_bytes": env.get("memory_total_bytes"),
        "memory_available_bytes": env.get("mem_available_bytes"),
        "memory_available_is_estimate": env.get("memory_available_is_estimate", False),
        "ac_online": env.get("ac_online"),
        "disk_free_bytes": disk_free,
        "disk_path": str(disk_path),
        "gpu": env.get("apple_gpu") if system == "Darwin" else nvidia_snapshot(),
    }
    if result["gpu"] is None:
        result["gpu"] = {"status": "unavailable", "reason": "not_measured", "devices": []}
    if system == "Darwin":
        result["apple_silicon"] = env.get("apple_silicon")
    if env.get("logical_cpus") is None and result["logical_cpus"] is not None:
        sources["logical_cpus"] = "os.cpu_count"
    for field in (
        "cpu_model",
        "logical_cpus",
        "physical_cpus",
        "memory_total_bytes",
        "memory_available_bytes",
        "ac_online",
    ):
        sources.setdefault(
            field, "runtime_environment" if environment is not None else "not_measured"
        )
        if result[field] is None:
            missing.setdefault(field, "not_reported")
        else:
            missing.pop(field, None)
    sources["disk_free_bytes"] = "shutil.disk_usage"
    sources["gpu"] = result["gpu"].get("source")
    if result["gpu"]["status"] == "unavailable":
        missing["gpu"] = result["gpu"].get("reason", "not_reported")
    return {**result, "sources": sources, "missing": missing}


def recommend(hardware, models, *, model_loaded=False):
    available = hardware.get("memory_available_bytes")
    metal = next(
        (
            device
            for device in hardware.get("gpu", {}).get("devices", [])
            if hardware.get("platform") == "Darwin"
            and hardware.get("apple_silicon") is True
            and device.get("metal_supported") is True
        ),
        None,
    )
    gpus = [
        g
        for g in hardware.get("gpu", {}).get("devices", [])
        if isinstance(g.get("memory_free_bytes"), int)
        and isinstance(g.get("memory_total_bytes"), int)
        and "index" in g
    ]
    candidates = []
    for model in models:
        if model.get("status") != "available" or model.get("chat_template_available") is False:
            continue
        size = model["size_bytes"]
        # This is a conservative screening estimate, never a promised runtime footprint.
        gpu_estimate = int(size * 1.2) + GPU_RESERVE
        startup = size + GIB
        reserve = max(GIB, min(8 * GIB, (available or 0) - startup - GIB // 2))
        host_fits = available is not None and available >= (
            reserve if model_loaded else startup + reserve
        )
        gpu = next(
            (
                g
                for g in sorted(gpus, key=lambda g: g["memory_free_bytes"], reverse=True)
                if g["memory_total_bytes"] >= gpu_estimate
                and g["memory_free_bytes"] >= (GIB // 2 if model_loaded else gpu_estimate)
            ),
            None,
        )
        mode = (
            "metal"
            if metal and host_fits
            else "cuda"
            if gpu and host_fits
            else "cpu"
            if host_fits
            else "blocked"
        )
        candidates.append(
            {
                "model_path": model["path"],
                "model_name": model["name"],
                "mode": mode,
                "gpu_index": gpu["index"] if mode == "cuda" else None,
                "gpu_name": gpu["name"]
                if mode == "cuda"
                else metal["name"]
                if mode == "metal"
                else None,
                "estimated_gpu_memory_bytes": None if mode == "metal" else gpu_estimate,
                "min_available_memory_bytes": reserve,
                "min_disk_bytes": 5 * GIB,
                "context_size": min(4096, model.get("context_length") or 4096),
                "threads": min(8, max(1, (hardware.get("logical_cpus") or 2) // 2)),
                "reason": "unified_memory_fit_estimate"
                if mode == "metal"
                else "gpu_and_memory_fit_estimate"
                if mode == "cuda"
                else "host_memory_fit_estimate"
                if mode == "cpu"
                else "host_memory_unavailable"
                if available is None
                else "insufficient_host_memory",
            }
        )
    candidates.sort(
        key=lambda c: (c["mode"] not in ("cuda", "metal"), c["mode"] == "blocked", c["model_path"])
    )
    selected = next((c for c in candidates if c["mode"] != "blocked"), None)
    reason = None
    if not models:
        reason = "no_local_model"
    elif not any(m.get("status") == "available" for m in models):
        reason = "invalid_or_unreadable_gguf"
    elif not candidates:
        reason = "chat_template_unavailable"
    elif available is None:
        reason = "host_memory_unavailable"
    elif selected is None:
        reason = "insufficient_host_memory"
    if candidates:
        if hardware.get("disk_free_bytes") is None:
            reason, selected = "output_disk_unavailable", None
        elif hardware["disk_free_bytes"] < 5 * GIB:
            reason, selected = "insufficient_output_disk", None
    return {
        "status": "recommended" if selected else "blocked",
        "selected": selected,
        "reason": reason,
        "candidates": candidates,
        "limitations": [
            "memory_estimates_require_live_preflight",
            "runtime_backend_and_model_compatibility_require_verification",
            "accelerator_execution_requires_verified_backend_and_service_binding",
        ],
    }


def discover_models(roots, model_path=None):
    paths = (
        [Path(model_path)]
        if model_path
        else sorted({p for root in roots for p in Path(root).rglob("*.gguf")})
    )
    models = []
    for path in paths:
        try:
            metadata = read_metadata(path)
            architecture = metadata.get("general.architecture")
            for key in (
                "general.name",
                "general.architecture",
                "tokenizer.chat_template",
                "tokenizer.chat_template.default",
            ):
                if metadata.get(key) is not None and not isinstance(metadata[key], str):
                    raise ValueError("invalid_gguf_metadata_type")
            context = metadata.get(str(architecture) + ".context_length")
            if context is not None and (type(context) is not int or context <= 0):
                raise ValueError("invalid_gguf_context_length")
            file_type = metadata.get("general.file_type")
            if file_type is not None and (type(file_type) is not int or file_type < 0):
                raise ValueError("invalid_gguf_file_type")
            models.append(
                {
                    "path": str(path.resolve()),
                    "name": metadata.get("general.name") or path.stem,
                    "size_bytes": path.stat().st_size,
                    "architecture": architecture,
                    "context_length": context,
                    "file_type": file_type,
                    "status": "available",
                    "chat_template_available": bool(
                        metadata.get("tokenizer.chat_template")
                        or metadata.get("tokenizer.chat_template.default")
                    ),
                }
            )
        except OSError, ValueError, UnicodeError:
            models.append(
                {
                    "path": str(path.resolve()),
                    "status": "unavailable",
                    "reason": "invalid_or_unreadable_gguf",
                }
            )
    return models


def platform_capabilities(system):
    return {
        "device_inspection": system in ("Darwin", "Linux", "Windows"),
        "live_single_run": system in ("Linux", "Windows", "Darwin"),
        "live_experiments": system in ("Linux", "Darwin"),
        "metal_backend_verified": False,
    }


def _next_steps(report):
    selected = report["recommendation"]["selected"]
    reason = report["recommendation"]["reason"]
    if selected is None:
        return [
            {
                "action": "provide_local_gguf" if reason == "no_local_model" else "resolve_blocker",
                "reason": reason,
            }
        ]
    if not report["platform_capabilities"]["live_single_run"]:
        return [{"action": "use_supported_live_platform", "reason": "macos_live_not_supported"}]
    return [
        {"action": "configure_and_bind_existing_service", "reason": "live_preflight_required"},
        {"action": "probe", "command": ["inferyard", "probe", "--config", "CONFIG.toml"]},
    ]


def inspect_device(roots, *, model_path=None, disk_path=None):
    hardware = hardware_snapshot(disk_path or Path.cwd())
    models = discover_models(roots, model_path)
    report = {
        "kind": "device_preflight.v1",
        "hardware": hardware,
        "models": models,
        "recommendation": recommend(hardware, models),
        "model_requests_sent": 0,
        "platform_capabilities": platform_capabilities(hardware.get("platform", platform.system())),
        "ready_to_run": False,
    }
    report["next_steps"] = _next_steps(report)
    return report


def _disk_ancestor(path):
    candidate = Path(path).resolve()
    while not candidate.exists() and candidate.parent != candidate:
        candidate = candidate.parent
    return candidate


def execute(request):
    from inferyard.application.types import CommandResult

    report = inspect_device(
        request.models_roots or [Path("artifacts")],
        model_path=request.model_path,
        disk_path=_disk_ancestor(request.out or Path.cwd()),
    )
    if request.out:
        request.out.mkdir(parents=True, exist_ok=False)
        atomic_bytes(request.out / "device-preflight.json", json_bytes(report))
    accepted = report["recommendation"]["status"] == "recommended"
    inspected = not report["models"] and not request.models_roots and request.model_path is None
    status = "recommended" if accepted else "inspected" if inspected else "blocked"
    return (0 if accepted or inspected else 2), CommandResult(
        request.command,
        status,
        "complete",
        evidence_dir=str(request.out) if request.out else None,
        details=report,
        limitations=(report["recommendation"]["reason"],) if status == "blocked" else (),
    )
