"""Read local hardware and GGUF candidates, then recommend an explicit execution mode."""

import os
import platform
import shutil
from datetime import UTC, datetime
from pathlib import Path

from inferyard.platforms.device_gpu import (
    _NVIDIA_BASE_QUERY as GPU_QUERY,
    cpu_features_snapshot,
    detect_gpus,
    nvidia_snapshot,
)
from inferyard.platforms.device_host import snapshot as host_snapshot
from inferyard.platforms.gguf_metadata import read_metadata
from inferyard.platforms.hardware_recommendation import recommend_starter

GIB = 1024**3
GPU_RESERVE = GIB

__all__ = [
    "GIB",
    "GPU_QUERY",
    "GPU_RESERVE",
    "discover_models",
    "existing_ancestor",
    "hardware_snapshot",
    "inspect_device",
    "nvidia_snapshot",
    "platform_capabilities",
    "recommend",
]


def hardware_snapshot(disk_path, environment=None, *, gpu_observation=None):
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
    if "cpu_features" in env:
        cpu_features = env["cpu_features"]
        feat_source = sources.get("cpu_features", "runtime_environment")
        feat_missing = {}
    else:
        cpu_features, feat_source, feat_missing = cpu_features_snapshot(system)
    os_version = env.get("os_version")
    if os_version is None and system != "Darwin":
        os_version = platform.release() or None
        sources.setdefault("os_version", "platform.release")
    result = {
        "observed_at": datetime.now(UTC).isoformat(),
        "platform": system,
        "os_version": os_version,
        "architecture": platform.machine(),
        "cpu_model": env.get("cpu_model"),
        "cpu_features": cpu_features,
        "logical_cpus": env.get("logical_cpus") or os.cpu_count(),
        "physical_cpus": env.get("physical_cpus"),
        "memory_total_bytes": env.get("memory_total_bytes"),
        "memory_available_bytes": env.get("mem_available_bytes"),
        "memory_available_is_estimate": env.get("memory_available_is_estimate", False),
        "page_file_total_bytes": env.get("page_file_total_bytes") if system == "Windows" else None,
        "ac_online": env.get("ac_online"),
        "disk_free_bytes": disk_free,
        "disk_path": str(disk_path),
        "gpu": env.get("apple_gpu")
        if system == "Darwin"
        else gpu_observation
        if gpu_observation is not None
        else detect_gpus(system),
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
    sources["cpu_features"] = feat_source
    for feat_name, reason in feat_missing.items():
        missing[f"cpu_features.{feat_name}"] = reason
    if result["os_version"] is None:
        missing.setdefault("os_version", "not_reported")
    else:
        missing.pop("os_version", None)
    if system == "Windows":
        sources.setdefault(
            "page_file_total_bytes",
            "runtime_environment" if environment is not None else "not_measured",
        )
        if result["page_file_total_bytes"] is None:
            missing.setdefault("page_file_total_bytes", "not_reported")
        else:
            missing.pop("page_file_total_bytes", None)
    else:
        sources["page_file_total_bytes"] = "platform.system"
        missing["page_file_total_bytes"] = "not_applicable_non_windows"
    sources["disk_free_bytes"] = "shutil.disk_usage"
    sources["gpu"] = result["gpu"].get("source")
    if result["gpu"]["status"] == "unavailable":
        missing["gpu"] = result["gpu"].get("reason", "not_reported")
    _benign_secondary = ("no_amd_devices", "nvidia_smi_not_found", "unsupported_platform")
    for vendor, reason in result["gpu"].get("vendor_reasons", {}).items():
        if result["gpu"]["status"] == "unavailable" and vendor == "nvidia":
            continue
        if reason not in _benign_secondary:
            missing[f"gpu.{vendor}"] = reason
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
        if g.get("vendor") in (None, "nvidia")
        and isinstance(g.get("memory_free_bytes"), int)
        and isinstance(g.get("memory_total_bytes"), int)
        and "index" in g
    ]
    candidates = []
    for model in models:
        if model.get("status") != "available" or model.get("chat_template_available") is False:
            continue
        size = model["size_bytes"]
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
    from inferyard.platforms.asset_scan import inspect_gguf_entries, parse_gguf_shard_filename

    if model_path:
        target = Path(model_path)
        parsed = parse_gguf_shard_filename(target.name)
        if parsed is not None:
            prefix, _idx, total = parsed
            paths = sorted(target.parent.glob(f"{prefix}-*-of-{total:05d}.gguf")) or [target]
            return inspect_gguf_entries(paths, metadata_reader=read_metadata)
        return inspect_gguf_entries([target], metadata_reader=read_metadata)
    paths = sorted({p for root in roots for p in Path(root).rglob("*.gguf")})
    return inspect_gguf_entries(paths, metadata_reader=read_metadata)


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
    return [
        {"action": "configure_and_bind_existing_service", "reason": "live_preflight_required"},
        {"action": "probe", "command": ["inferyard", "probe", "--config", "CONFIG.toml"]},
    ]


def _disk_free(path):
    try:
        return shutil.disk_usage(path).free, None
    except OSError:
        return None, "disk_usage_unavailable"


def _attach_models_disk(hardware, roots, *, model_path=None, selected_model_path=None):
    if "models_disk_free_bytes" in hardware:
        return
    sources = hardware.setdefault("sources", {})
    missing = hardware.setdefault("missing", {})
    sources["models_disk_free_bytes"] = "shutil.disk_usage"

    if model_path is not None:
        target = existing_ancestor(model_path)
        free, err = _disk_free(target)
        hardware["models_disk_path"] = str(target)
        hardware["models_disk_free_bytes"] = free
        missing.pop("models_disk_path", None)
        if err is None:
            missing.pop("models_disk_free_bytes", None)
        else:
            missing["models_disk_free_bytes"] = err
        return

    if selected_model_path is not None:
        target = existing_ancestor(selected_model_path)
        free, err = _disk_free(target)
        hardware["models_disk_path"] = str(target)
        hardware["models_disk_free_bytes"] = free
        missing.pop("models_disk_path", None)
        if err is None:
            missing.pop("models_disk_free_bytes", None)
        else:
            missing["models_disk_free_bytes"] = err
        return

    if not roots:
        return

    targets = [existing_ancestor(root) for root in roots]
    root_entries = []
    devs = set()
    for target in targets:
        free, err = _disk_free(target)
        entry = {"path": str(target), "disk_free_bytes": free}
        if err is not None:
            entry["missing"] = err
        root_entries.append(entry)
        try:
            devs.add(target.stat().st_dev)
        except OSError:
            devs.add(None)

    if len(targets) == 1 or (len(devs) == 1 and None not in devs):
        first = root_entries[0]
        hardware["models_disk_path"] = first["path"]
        hardware["models_disk_free_bytes"] = first["disk_free_bytes"]
        missing.pop("models_disk_path", None)
        if "missing" in first:
            missing["models_disk_free_bytes"] = first["missing"]
        else:
            missing.pop("models_disk_free_bytes", None)
    else:
        hardware["models_disk_path"] = None
        hardware["models_disk_free_bytes"] = None
        hardware["models_disk_roots"] = root_entries
        missing["models_disk_path"] = "multiple_models_roots_unselected"
        missing["models_disk_free_bytes"] = "multiple_models_roots_unselected"


def inspect_device(roots, *, model_path=None, disk_path=None):
    hardware = hardware_snapshot(disk_path or Path.cwd())
    models = discover_models(roots, model_path)
    recommendation = recommend(hardware, models)
    selected_path = (recommendation.get("selected") or {}).get("model_path")
    _attach_models_disk(
        hardware,
        roots,
        model_path=model_path,
        selected_model_path=selected_path,
    )
    report = {
        "kind": "device_preflight.v1",
        "hardware": hardware,
        "models": models,
        "recommendation": recommendation,
        "starter_recommendation": recommend_starter(hardware),
        "model_requests_sent": 0,
        "platform_capabilities": platform_capabilities(hardware.get("platform", platform.system())),
        "ready_to_run": False,
    }
    report["next_steps"] = _next_steps(report)
    return report


def existing_ancestor(path):
    candidate = Path(path).resolve()
    while not candidate.exists() and candidate.parent != candidate:
        candidate = candidate.parent
    return candidate
