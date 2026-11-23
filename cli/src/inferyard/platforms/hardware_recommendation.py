"""Hardware-aware starter engine and model recommendation (PolyStrata-first with llama.cpp fallback)."""

from __future__ import annotations

from collections.abc import Callable
import re

from inferyard.platforms.polystrata_sizing import (
    DEFAULT_RUNTIME_DISK_BYTES,
    GIB,
    load_starter_catalog,
    polystrata_total_disk_bytes,
)

STARTER_DEFINITION = "starter-recommendation.v1"
_MIN_NVIDIA_COMPUTE = (7, 5)
_MIN_NVIDIA_DRIVER_MAJOR = 580
_MIN_MACOS_METAL = (15, 0)
_PC_RAM_TOLERANCE = int(1.5 * GIB)
# Real nvidia-smi reports slightly under nominal GiB (e.g. 8188 MiB on 8 GB, 12227 MiB on 12 GB).
_VRAM_TIER_TOLERANCE = 512 * 1024**2
_POLY_RAM_BESIDE_GB = 10.0
_POLY_RAM_TOLERANCE_GB = 4.0
_GIB_TO_GB = 1.073741824
_MAC_GPU_SHARE = 0.75
_MAC_RESIDENT_LEAVE_SHARE = 0.30
_MAC_BIG_RAM_BYTES = 64 * GIB
_SUPPORTED_AMD_ARCH = frozenset(
    {"gfx1030", "gfx1100", "gfx1101", "gfx1151", "gfx1200", "gfx1201"}
)
_VERSION_PART_RE = re.compile(r"\d+")

_load_catalog = load_starter_catalog


def _valid_bytes(value) -> int | None:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        return None
    return value


def _parse_version(raw) -> tuple[int, ...] | None:
    if not isinstance(raw, str) or not raw.strip():
        return None
    parts = _VERSION_PART_RE.findall(raw.strip())
    if not parts:
        return None
    return tuple(int(p) for p in parts[:3])


def _effective_disk(hardware: dict) -> tuple[int | None, list[str]]:
    notes: list[str] = []
    missing = hardware.get("missing") or {}
    if "models_disk_free_bytes" in hardware:
        models_disk = _valid_bytes(hardware.get("models_disk_free_bytes"))
        if models_disk is not None:
            return models_disk, notes
        reason = missing.get("models_disk_free_bytes")
        if reason:
            notes.append(reason)
        return None, notes
    return _valid_bytes(hardware.get("disk_free_bytes")), notes


def _effective_memory(hardware: dict) -> tuple[int | None, int | None]:
    total = _valid_bytes(hardware.get("memory_total_bytes"))
    available = _valid_bytes(hardware.get("memory_available_bytes"))
    return total, available


def _mac_metal_budget_bytes(ram_total_bytes: int) -> int:
    """Upstream metal/setup_mac.py resident working-set budget (in decimal bytes)."""
    ws = int(ram_total_bytes * _MAC_GPU_SHARE)
    if ram_total_bytes >= _MAC_BIG_RAM_BYTES:
        return min(ws, int(ram_total_bytes * (1.0 - _MAC_RESIDENT_LEAVE_SHARE)))
    return ws


def _collect_polystrata_accelerators(
    hardware: dict,
) -> tuple[list[tuple[str, dict]], list[str]]:
    system = hardware.get("platform")
    devices = (hardware.get("gpu") or {}).get("devices") or []
    notes: list[str] = []

    if system == "Darwin":
        if hardware.get("apple_silicon") is not True:
            return [], ["apple_silicon_required"]
        metal_dev = next((d for d in devices if d.get("metal_supported") is True), None)
        if metal_dev is None:
            return [], ["metal_not_supported"]
        os_ver = _parse_version(hardware.get("os_version"))
        if os_ver is None:
            return [], ["macos_version_unverified"]
        if os_ver < _MIN_MACOS_METAL:
            return [], ["macos_version_below_15"]
        ram_total = _valid_bytes(hardware.get("memory_total_bytes"))
        return (
            [
                (
                    "metal",
                    {
                        "vendor": "apple",
                        "name": metal_dev.get("name"),
                        "vram_bytes": None,
                        "unified_memory_bytes": ram_total,
                    },
                )
            ],
            [],
        )

    if system not in ("Linux", "Windows"):
        return [], ["unsupported_platform"]

    best_by_backend: dict[str, tuple[int, dict]] = {}
    for dev in devices:
        vendor = dev.get("vendor")
        if vendor in (None, "nvidia"):
            vram = _valid_bytes(dev.get("memory_total_bytes"))
            if not vram:
                notes.append("nvidia_vram_unverified")
                continue
            cap = _parse_version(dev.get("compute_capability"))
            if cap is None:
                notes.append("nvidia_compute_capability_unverified")
                continue
            if len(cap) < 2 or (cap[0], cap[1]) < _MIN_NVIDIA_COMPUTE:
                notes.append("nvidia_compute_capability_unsupported")
                continue
            drv = _parse_version(dev.get("driver_version"))
            if drv is None:
                notes.append("nvidia_driver_version_unverified")
                continue
            if drv[0] < _MIN_NVIDIA_DRIVER_MAJOR:
                notes.append("nvidia_driver_too_old")
                continue
            summary = {"vendor": "nvidia", "name": dev.get("name"), "vram_bytes": vram}
            prev = best_by_backend.get("cuda")
            if prev is None or vram > prev[0]:
                best_by_backend["cuda"] = (vram, summary)
        elif vendor == "amd":
            vram = _valid_bytes(dev.get("memory_total_bytes"))
            if not vram:
                notes.append("amd_vram_unverified")
                continue
            arch = dev.get("arch_family")
            if arch not in _SUPPORTED_AMD_ARCH:
                notes.append("amd_arch_unsupported")
                continue
            summary = {
                "vendor": "amd",
                "name": dev.get("name"),
                "vram_bytes": vram,
                "arch_family": arch,
            }
            prev = best_by_backend.get("hip")
            if prev is None or vram > prev[0]:
                best_by_backend["hip"] = (vram, summary)

    if not best_by_backend:
        return [], list(dict.fromkeys(notes))

    ordered = sorted(
        best_by_backend.items(),
        key=lambda item: (item[1][0], 1 if item[0] == "cuda" else 0),
        reverse=True,
    )
    return [(backend, entry[1]) for backend, entry in ordered], []


def _poly_ram_need_gb(model: dict, vram_bytes: int | None) -> float:
    """Upstream setup.py poly_ram_need() in decimal GB using measured VRAM in GiB."""
    vram_gib = (vram_bytes or 0) / GIB
    beside_gib = float(model.get("vram_beside_gib", 12.0))
    arena_gb = int(model["arena_bytes"]) / 1e9
    held_gb = min(arena_gb, max(0.0, vram_gib - beside_gib) * _GIB_TO_GB)
    return arena_gb - held_gb + _POLY_RAM_BESIDE_GB


def _estimated_ram_bytes(model: dict, backend: str, vram_bytes: int | None) -> int:
    if backend == "metal":
        return int(model["mac_resident_bytes"])
    if model.get("engine_core") == "strata":
        return int(model["ram_needed_bytes"])
    return int(_poly_ram_need_gb(model, vram_bytes) * 1e9)


def _recommended_context(backend: str, capacity_bytes: int | None) -> int:
    cap = capacity_bytes or 0
    if backend == "metal":
        # Upstream metal/setup_mac.py: 32768 under 96 GiB, 65536 from 96 GiB.
        return 32768 if cap < 96 * GIB else 65536
    return (
        32768
        if cap < 14 * GIB - _VRAM_TIER_TOLERANCE
        else 65536
        if cap < 20 * GIB - _VRAM_TIER_TOLERANCE
        else 131072
    )


def _fallback_accelerator(hardware: dict) -> tuple[str, dict | None]:
    system = hardware.get("platform")
    devices = (hardware.get("gpu") or {}).get("devices") or []
    if system == "Darwin" and hardware.get("apple_silicon") is True:
        metal_dev = next((d for d in devices if d.get("metal_supported") is True), None)
        if metal_dev is not None:
            return (
                "metal",
                {
                    "vendor": "apple",
                    "name": metal_dev.get("name"),
                    "vram_bytes": None,
                    "unified_memory_bytes": _valid_bytes(hardware.get("memory_total_bytes")),
                },
            )
    nv_devs = [
        d
        for d in devices
        if d.get("vendor") in (None, "nvidia") and _valid_bytes(d.get("memory_total_bytes"))
    ]
    if nv_devs:
        best = max(nv_devs, key=lambda d: _valid_bytes(d.get("memory_total_bytes")) or 0)
        return (
            "cuda",
            {
                "vendor": "nvidia",
                "name": best.get("name"),
                "vram_bytes": _valid_bytes(best.get("memory_total_bytes")),
            },
        )
    return "cpu", None


def _evaluate_polystrata(
    hardware: dict,
    catalog: dict,
    accelerators: list[tuple[str, dict]],
    ram_total: int,
    disk_free: int,
) -> tuple[list[dict], list[str]]:
    system = hardware.get("platform")
    runtime_disk_free = _valid_bytes(hardware.get("disk_free_bytes"))
    if (
        "models_disk_free_bytes" in hardware
        and runtime_disk_free is not None
        and runtime_disk_free < DEFAULT_RUNTIME_DISK_BYTES
    ):
        return [], ["insufficient_disk"]
    avx512 = (hardware.get("cpu_features") or {}).get("avx512")
    best_by_model: dict[str, dict] = {}
    rejection_reasons: list[str] = []

    for backend, gpu in accelerators:
        vram = gpu.get("vram_bytes")
        metal_budget = _mac_metal_budget_bytes(ram_total) if backend == "metal" else None
        for model in catalog["polystrata_models"]:
            supported_backends = model.get("platforms", {}).get(system, [])
            if backend not in supported_backends:
                continue
            if backend != "metal":
                min_vram = int(model["min_vram_bytes"])
                if vram is None or vram < max(0, min_vram - _VRAM_TIER_TOLERANCE):
                    rejection_reasons.append("insufficient_vram")
                    continue
                if model.get("engine_core") == "strata-poly":
                    beside_gib = float(model.get("vram_beside_gib", 12.0))
                    if (vram / GIB) < beside_gib + 2.0:
                        rejection_reasons.append("insufficient_vram")
                        continue
            est_ram = _estimated_ram_bytes(model, backend, vram)
            if backend == "metal":
                ram_ok = metal_budget is not None and est_ram <= metal_budget
            elif model.get("engine_core") == "strata-poly":
                ram_ok = (ram_total / GIB) >= _poly_ram_need_gb(model, vram) - _POLY_RAM_TOLERANCE_GB
            else:
                ram_ok = ram_total >= max(0, est_ram - _PC_RAM_TOLERANCE)
            if not ram_ok:
                rejection_reasons.append("insufficient_ram")
                continue
            need_disk = polystrata_total_disk_bytes(
                model, system=str(system), avx512=avx512
            )
            if disk_free < need_disk:
                rejection_reasons.append("insufficient_disk")
                continue
            capacity = ram_total if backend == "metal" else vram
            candidate = {
                "engine": "polystrata",
                "backend": backend,
                "model_id": model["id"],
                "model_title": model["title"],
                "family": model["family"],
                "size": model["size"],
                "engine_core": model["engine_core"],
                "model_format": model["model_format"],
                "fit_level": "fits",
                "recommended_context": _recommended_context(backend, capacity),
                "estimated_download_bytes": model["download_bytes"],
                "estimated_ram_bytes": est_ram,
                "quality_rank": model["quality_rank"],
                "_gpu": gpu,
                "_vram_sort": vram or 0,
                "_backend_prio": 1 if backend == "cuda" else 0,
            }
            prev = best_by_model.get(model["id"])
            if prev is None or (candidate["_vram_sort"], candidate["_backend_prio"]) > (
                prev["_vram_sort"],
                prev["_backend_prio"],
            ):
                best_by_model[model["id"]] = candidate

    fitting = sorted(
        best_by_model.values(),
        key=lambda item: (item["quality_rank"], item["_vram_sort"], item["_backend_prio"]),
        reverse=True,
    )
    return fitting, list(dict.fromkeys(rejection_reasons))


def _evaluate_fallbacks(
    hardware: dict, catalog: dict, ram_capacity: int, disk_free: int
) -> tuple[list[dict], list[str], dict | None]:
    backend, fb_gpu = _fallback_accelerator(hardware)
    fitting: list[dict] = []
    reasons: list[str] = []
    for model in catalog["fallback_models"]:
        if ram_capacity < model["min_ram_bytes"]:
            reasons.append("insufficient_ram")
            continue
        if disk_free < model["min_disk_bytes"]:
            reasons.append("insufficient_disk")
            continue
        fitting.append(
            {
                "engine": model["engine"],
                "backend": backend,
                "model_id": model["id"],
                "model_title": model["title"],
                "family": "llama-cpp",
                "size": None,
                "engine_core": None,
                "model_format": model["model_format"],
                "fit_level": "fits",
                "recommended_context": model["recommended_context"],
                "estimated_download_bytes": model["download_bytes"],
                "estimated_ram_bytes": model["min_ram_bytes"],
                "quality_rank": model["quality_rank"],
            }
        )
    fitting.sort(key=lambda item: item["quality_rank"], reverse=True)
    return fitting, list(dict.fromkeys(reasons)), fb_gpu


def _public_item(item: dict) -> dict:
    return {k: v for k, v in item.items() if k != "quality_rank" and not k.startswith("_")}


def _blocked_result(reason: str, disk_free: int | None, notes: list[str]) -> dict:
    return {
        "definition": STARTER_DEFINITION,
        "status": "blocked",
        "engine": None,
        "backend": None,
        "model_id": None,
        "model_title": None,
        "family": None,
        "size": None,
        "engine_core": None,
        "model_format": None,
        "fit_level": None,
        "recommended_context": None,
        "estimated_download_bytes": None,
        "estimated_ram_bytes": None,
        "disk_free_bytes": disk_free,
        "reason": reason,
        "gpu": None,
        "notes": list(dict.fromkeys(notes)),
        "alternatives": [],
    }


def _select_polystrata_alternatives(
    chosen: dict, remaining: list[dict], *, limit: int = 3
) -> list[dict]:
    """Ensure high-RAM hosts still see at least one lightweight strata-poly alternative."""
    selected = list(remaining[:limit])
    if (
        chosen.get("engine_core") != "strata-poly"
        and selected
        and not any(item.get("engine_core") == "strata-poly" for item in selected)
    ):
        lightweight = next(
            (item for item in remaining[limit:] if item.get("engine_core") == "strata-poly"),
            None,
        )
        if lightweight is not None:
            selected[-1] = lightweight
    return selected


def recommend_starter(
    hardware: dict,
    *,
    catalog: dict | None = None,
    xcode_clt_check: Callable[[], bool] | None = None,
) -> dict:
    """Recommend default starter engine and model from observed hardware."""
    cat = catalog if catalog is not None else _load_catalog()
    disk_free, notes = _effective_disk(hardware)
    ram_total, ram_available = _effective_memory(hardware)

    if disk_free is None:
        return _blocked_result("disk_space_unavailable", None, notes)

    ram_capacity = ram_total if ram_total is not None else ram_available
    if ram_capacity is None:
        return _blocked_result("host_memory_unavailable", disk_free, notes)

    accelerators, accel_notes = _collect_polystrata_accelerators(hardware)
    notes.extend(accel_notes)

    poly_fits: list[dict] = []
    poly_rejects: list[str] = []
    if accelerators and ram_total is not None:
        poly_fits, poly_rejects = _evaluate_polystrata(
            hardware, cat, accelerators, ram_total, disk_free
        )
    elif accelerators and ram_total is None:
        notes.append("total_memory_unverified")

    fallback_fits, fallback_rejects, fb_gpu = _evaluate_fallbacks(
        hardware, cat, ram_capacity, disk_free
    )

    if poly_fits:
        chosen = poly_fits[0]
        if hardware.get("platform") == "Darwin" and hardware.get("apple_silicon") is True:
            clt_installed = hardware.get("xcode_clt_installed")
            if clt_installed is None and xcode_clt_check is not None:
                clt_installed = bool(xcode_clt_check())
            if clt_installed is False:
                notes.append("xcode_clt_missing")
        if (hardware.get("cpu_features") or {}).get("avx2") is False:
            notes.append("cpu_missing_avx2_experimental_build")
        if hardware.get("platform") == "Windows":
            page_file = _valid_bytes(hardware.get("page_file_total_bytes"))
            if page_file is not None and page_file < 16 * GIB:
                notes.append("windows_page_file_low")
        if ram_available is not None and ram_available < int(chosen["estimated_ram_bytes"] * 0.6):
            notes.append("host_available_memory_tight")

        poly_alts = _select_polystrata_alternatives(chosen, poly_fits[1:])
        alternatives = [_public_item(item) for item in poly_alts]
        if fallback_fits:
            alternatives.append(_public_item(fallback_fits[0]))
        return {
            "definition": STARTER_DEFINITION,
            "status": "recommended",
            **_public_item(chosen),
            "disk_free_bytes": disk_free,
            "reason": "polystrata_hardware_fit",
            "gpu": chosen["_gpu"],
            "notes": list(dict.fromkeys(notes)),
            "alternatives": alternatives,
        }

    if fallback_fits:
        chosen = fallback_fits[0]
        if ram_available is not None and ram_available < int(chosen["estimated_ram_bytes"] * 0.6):
            notes.append("host_available_memory_tight")
        if "insufficient_disk" in poly_rejects:
            reason = "fallback_llama_cpp_insufficient_disk_for_polystrata"
        elif "insufficient_ram" in poly_rejects:
            reason = "fallback_llama_cpp_insufficient_ram_for_polystrata"
        elif "insufficient_vram" in poly_rejects:
            reason = "fallback_llama_cpp_insufficient_vram_for_polystrata"
        else:
            reason = "fallback_llama_cpp_no_compatible_polystrata_gpu"
        alternatives = [_public_item(item) for item in fallback_fits[1:3]]
        return {
            "definition": STARTER_DEFINITION,
            "status": "recommended",
            **_public_item(chosen),
            "disk_free_bytes": disk_free,
            "reason": reason,
            "gpu": fb_gpu,
            "notes": list(dict.fromkeys(notes)),
            "alternatives": alternatives,
        }

    block_reason = (
        "insufficient_disk_space"
        if "insufficient_disk" in fallback_rejects
        else "insufficient_host_memory"
    )
    return _blocked_result(block_reason, disk_free, notes)
