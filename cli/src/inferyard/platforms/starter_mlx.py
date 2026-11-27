"""Apple Silicon mlx-lm candidate evaluation for hardware-aware starter recommendations."""

from __future__ import annotations

from inferyard.platforms.starter_ranking import fallback_fit_level


def evaluate_mlx_models(
    hardware: dict,
    catalog: dict,
    ram_capacity: int,
    disk_free: int,
    *,
    ram_available: int | None = None,
    min_macos_metal: tuple[int, int] = (15, 0),
    parse_version=None,
    valid_bytes=None,
) -> tuple[list[dict], list[str], dict | None]:
    """Evaluate Apple Silicon mlx-lm starter candidates against observed RAM and disk."""
    if hardware.get("platform") != "Darwin" or hardware.get("apple_silicon") is not True:
        return [], [], None
    devices = (hardware.get("gpu") or {}).get("devices") or []
    metal_dev = next((d for d in devices if d.get("metal_supported") is True), None)
    if metal_dev is None:
        return [], [], None
    os_ver = parse_version(hardware.get("os_version")) if parse_version else None
    if os_ver is None or os_ver < min_macos_metal:
        return [], [], None
    mlx_gpu = {
        "vendor": "apple",
        "name": metal_dev.get("name"),
        "vram_bytes": None,
        "unified_memory_bytes": valid_bytes(hardware.get("memory_total_bytes"))
        if valid_bytes
        else None,
    }
    fitting: list[dict] = []
    reasons: list[str] = []
    for model in catalog.get("mlx_models") or []:
        if ram_capacity < model["min_ram_bytes"]:
            reasons.append("insufficient_ram")
            continue
        if disk_free < model["min_disk_bytes"]:
            reasons.append("insufficient_disk")
            continue
        fit_level = fallback_fit_level(
            ram_capacity=ram_capacity,
            ram_available=ram_available,
            min_ram_bytes=model["min_ram_bytes"],
            disk_free=disk_free,
            min_disk_bytes=model["min_disk_bytes"],
        )
        fitting.append(
            {
                "engine": model["engine"],
                "backend": "metal",
                "model_id": model["id"],
                "model_title": model["title"],
                "family": "mlx-lm",
                "size": None,
                "engine_core": None,
                "model_format": model["model_format"],
                "fit_level": fit_level,
                "recommended_context": model["recommended_context"],
                "estimated_download_bytes": model["download_bytes"],
                "estimated_ram_bytes": model["min_ram_bytes"],
                "quality_rank": model["quality_rank"],
            }
        )
    fitting.sort(key=lambda item: item["quality_rank"], reverse=True)
    return fitting, list(dict.fromkeys(reasons)), mlx_gpu


def evaluate_fallback_models(
    hardware: dict,
    catalog: dict,
    ram_capacity: int,
    disk_free: int,
    *,
    ram_available: int | None = None,
    fallback_accelerator=None,
) -> tuple[list[dict], list[str], dict | None]:
    """Evaluate llama.cpp fallback starter models against observed RAM and disk."""
    backend, fb_gpu = fallback_accelerator(hardware)
    fitting: list[dict] = []
    reasons: list[str] = []
    for model in catalog["fallback_models"]:
        if ram_capacity < model["min_ram_bytes"]:
            reasons.append("insufficient_ram")
            continue
        if disk_free < model["min_disk_bytes"]:
            reasons.append("insufficient_disk")
            continue
        fit_level = fallback_fit_level(
            ram_capacity=ram_capacity,
            ram_available=ram_available,
            min_ram_bytes=model["min_ram_bytes"],
            disk_free=disk_free,
            min_disk_bytes=model["min_disk_bytes"],
        )
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
                "fit_level": fit_level,
                "recommended_context": model["recommended_context"],
                "estimated_download_bytes": model["download_bytes"],
                "estimated_ram_bytes": model["min_ram_bytes"],
                "quality_rank": model["quality_rank"],
            }
        )
    fitting.sort(key=lambda item: item["quality_rank"], reverse=True)
    return fitting, list(dict.fromkeys(reasons)), fb_gpu

