"""Hardware-aware starter engine and model recommendation (PolyStrata-first with llama.cpp fallback)."""

from __future__ import annotations

from functools import lru_cache
from importlib.resources import files
import re

from inferyard.contracts.validation import strict_json_loads

GIB = 1024**3
STARTER_DEFINITION = "starter-recommendation.v1"
_MIN_NVIDIA_COMPUTE = (7, 5)
_MIN_NVIDIA_DRIVER_MAJOR = 580
_MIN_MACOS_METAL = (15, 0)
_PC_RAM_TOLERANCE = int(1.5 * GIB)
_SUPPORTED_AMD_ARCH = frozenset(
    {"gfx1030", "gfx1100", "gfx1101", "gfx1151", "gfx1200", "gfx1201"}
)
_VERSION_PART_RE = re.compile(r"\d+")


@lru_cache(maxsize=1)
def _load_catalog() -> dict:
    raw = files("inferyard").joinpath("data", "starter_catalog.json").read_text(encoding="utf-8")
    return strict_json_loads(raw)


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
        if missing.get("models_disk_free_bytes") == "multiple_models_roots_unselected":
            roots = hardware.get("models_disk_roots")
            if isinstance(roots, list) and roots and isinstance(roots[0], dict):
                primary = _valid_bytes(roots[0].get("disk_free_bytes"))
                notes.append("models_disk_from_primary_models_root")
                return primary, notes
            notes.append("models_disk_unselected_using_workspace_disk")
            return _valid_bytes(hardware.get("disk_free_bytes")), notes
        return None, notes
    return _valid_bytes(hardware.get("disk_free_bytes")), notes


def _effective_memory(hardware: dict) -> tuple[int | None, int | None]:
    total = _valid_bytes(hardware.get("memory_total_bytes"))
    available = _valid_bytes(hardware.get("memory_available_bytes"))
    return total, available


def _select_polystrata_accelerator(hardware: dict) -> tuple[str | None, dict | None, list[str]]:
    system = hardware.get("platform")
    devices = (hardware.get("gpu") or {}).get("devices") or []
    notes: list[str] = []

    if system == "Darwin":
        if hardware.get("apple_silicon") is not True:
            return None, None, ["apple_silicon_required"]
        metal_dev = next((d for d in devices if d.get("metal_supported") is True), None)
        if metal_dev is None:
            return None, None, ["metal_not_supported"]
        os_ver = _parse_version(hardware.get("os_version"))
        if os_ver is None:
            return None, None, ["macos_version_unverified"]
        if os_ver < _MIN_MACOS_METAL:
            return None, None, ["macos_version_below_15"]
        ram_total = _valid_bytes(hardware.get("memory_total_bytes"))
        return (
            "metal",
            {
                "vendor": "apple",
                "name": metal_dev.get("name"),
                "vram_bytes": None,
                "unified_memory_bytes": ram_total,
            },
            notes,
        )

    if system not in ("Linux", "Windows"):
        return None, None, ["unsupported_platform"]

    candidates: list[tuple[int, int, str, dict]] = []
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
            candidates.append(
                (
                    vram,
                    1,
                    "cuda",
                    {"vendor": "nvidia", "name": dev.get("name"), "vram_bytes": vram},
                )
            )
        elif vendor == "amd":
            vram = _valid_bytes(dev.get("memory_total_bytes"))
            if not vram:
                notes.append("amd_vram_unverified")
                continue
            arch = dev.get("arch_family")
            if arch not in _SUPPORTED_AMD_ARCH:
                notes.append("amd_arch_unsupported")
                continue
            candidates.append(
                (
                    vram,
                    0,
                    "hip",
                    {
                        "vendor": "amd",
                        "name": dev.get("name"),
                        "vram_bytes": vram,
                        "arch_family": arch,
                    },
                )
            )

    if not candidates:
        return None, None, list(dict.fromkeys(notes))

    candidates.sort(key=lambda item: (item[0], item[1]), reverse=True)
    _vram, _prio, backend, summary = candidates[0]
    return backend, summary, []


def _required_disk_bytes(model: dict) -> int:
    if "min_disk_bytes" in model:
        return int(model["min_disk_bytes"])
    extra = 5 * GIB if model.get("engine_core") == "strata" else 2 * GIB
    return int(model["download_bytes"] * model.get("disk_multiplier", 1.15)) + extra


def _estimated_ram_bytes(model: dict, backend: str, vram_bytes: int | None) -> int:
    if backend == "metal":
        return int(model["mac_min_unified_bytes"])
    if model.get("engine_core") == "strata":
        return int(model["ram_needed_bytes"])
    beside = int(model.get("beside_vram_bytes", 4 * GIB))
    overhead = int(model.get("ram_overhead_bytes", 5 * GIB))
    arena = int(model["arena_bytes"])
    held = max(0, (vram_bytes or 0) - beside)
    return max(overhead, arena - held + overhead)


def _recommended_context(backend: str, capacity_bytes: int | None) -> int:
    cap = capacity_bytes or 0
    if backend == "metal":
        return 32768 if cap < 36 * GIB else 65536 if cap < 64 * GIB else 131072
    return 32768 if cap < 14 * GIB else 65536 if cap < 20 * GIB else 131072


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
    backend: str,
    gpu: dict,
    ram_total: int,
    disk_free: int,
) -> tuple[list[dict], list[str]]:
    system = hardware.get("platform")
    vram = gpu.get("vram_bytes")
    fitting: list[dict] = []
    rejection_reasons: list[str] = []

    for model in catalog["polystrata_models"]:
        supported_backends = model.get("platforms", {}).get(system, [])
        if backend not in supported_backends:
            continue
        if backend != "metal" and (vram is None or vram < model["min_vram_bytes"]):
            rejection_reasons.append("insufficient_vram")
            continue
        est_ram = _estimated_ram_bytes(model, backend, vram)
        ram_ok = (
            ram_total >= est_ram
            if backend == "metal"
            else ram_total >= max(0, est_ram - _PC_RAM_TOLERANCE)
        )
        if not ram_ok:
            rejection_reasons.append("insufficient_ram")
            continue
        need_disk = _required_disk_bytes(model)
        if disk_free < need_disk:
            rejection_reasons.append("insufficient_disk")
            continue
        capacity = ram_total if backend == "metal" else vram
        fitting.append(
            {
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
            }
        )

    fitting.sort(key=lambda item: item["quality_rank"], reverse=True)
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


def _strip_rank(item: dict) -> dict:
    return {k: v for k, v in item.items() if k != "quality_rank"}


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
        "notes": notes,
        "alternatives": [],
    }


def recommend_starter(hardware: dict, *, catalog: dict | None = None) -> dict:
    """Recommend default starter engine and model from observed hardware."""
    cat = catalog if catalog is not None else _load_catalog()
    disk_free, notes = _effective_disk(hardware)
    ram_total, ram_available = _effective_memory(hardware)

    if disk_free is None:
        return _blocked_result("disk_space_unavailable", None, notes)

    ram_capacity = ram_total if ram_total is not None else ram_available
    if ram_capacity is None:
        return _blocked_result("host_memory_unavailable", disk_free, notes)

    backend, gpu, accel_notes = _select_polystrata_accelerator(hardware)
    notes.extend(accel_notes)

    poly_fits: list[dict] = []
    poly_rejects: list[str] = []
    if backend is not None and gpu is not None and ram_total is not None:
        poly_fits, poly_rejects = _evaluate_polystrata(
            hardware, cat, backend, gpu, ram_total, disk_free
        )
    elif backend is not None and ram_total is None:
        notes.append("total_memory_unverified")

    fallback_fits, fallback_rejects, fb_gpu = _evaluate_fallbacks(
        hardware, cat, ram_capacity, disk_free
    )

    if poly_fits:
        chosen = poly_fits[0]
        if (hardware.get("cpu_features") or {}).get("avx2") is False:
            notes.append("cpu_missing_avx2_experimental_build")
        if hardware.get("platform") == "Windows":
            page_file = _valid_bytes(hardware.get("page_file_total_bytes"))
            if page_file is not None and page_file < 16 * GIB:
                notes.append("windows_page_file_low")
        if ram_available is not None and ram_available < int(chosen["estimated_ram_bytes"] * 0.6):
            notes.append("host_available_memory_tight")

        alternatives = [_strip_rank(item) for item in poly_fits[1:4]]
        if fallback_fits:
            alternatives.append(_strip_rank(fallback_fits[0]))
        return {
            "definition": STARTER_DEFINITION,
            "status": "recommended",
            **_strip_rank(chosen),
            "disk_free_bytes": disk_free,
            "reason": "polystrata_hardware_fit",
            "gpu": gpu,
            "notes": list(dict.fromkeys(notes)),
            "alternatives": alternatives,
        }

    if fallback_fits:
        chosen = fallback_fits[0]
        if "insufficient_disk" in poly_rejects:
            reason = "fallback_llama_cpp_insufficient_disk_for_polystrata"
        elif "insufficient_ram" in poly_rejects:
            reason = "fallback_llama_cpp_insufficient_ram_for_polystrata"
        elif "insufficient_vram" in poly_rejects:
            reason = "fallback_llama_cpp_insufficient_vram_for_polystrata"
        else:
            reason = "fallback_llama_cpp_no_compatible_polystrata_gpu"
        alternatives = [_strip_rank(item) for item in fallback_fits[1:3]]
        return {
            "definition": STARTER_DEFINITION,
            "status": "recommended",
            **_strip_rank(chosen),
            "disk_free_bytes": disk_free,
            "reason": reason,
            "gpu": fb_gpu if fb_gpu is not None else gpu,
            "notes": list(dict.fromkeys(notes)),
            "alternatives": alternatives,
        }

    block_reason = (
        "insufficient_disk_space"
        if "insufficient_disk" in fallback_rejects
        else "insufficient_host_memory"
    )
    return _blocked_result(block_reason, disk_free, list(dict.fromkeys(notes)))
