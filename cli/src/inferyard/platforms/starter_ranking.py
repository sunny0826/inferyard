"""Fit-level grading and diverse alternative selection for starter recommendations."""

from __future__ import annotations

from inferyard.platforms.polystrata_sizing import GIB

_POLY_DISK_COMFORT_HEADROOM = 10 * GIB
_FALLBACK_DISK_COMFORT_HEADROOM = 2 * GIB
_PC_RAM_COMFORT_HEADROOM = 2 * GIB
_PC_VRAM_COMFORT_HEADROOM = 1 * GIB


def polystrata_fit_level(
    *,
    backend: str,
    engine_core: str,
    ram_total: int,
    ram_available: int | None,
    est_ram: int,
    disk_free: int,
    need_disk: int,
    metal_budget: int | None = None,
    vram: int | None = None,
    min_vram: int = 0,
    beside_gib: float = 0.0,
    poly_ram_need_gb: float | None = None,
) -> str:
    """Return 'tight' when the model fits only near hardware headroom limits, else 'fits'."""
    if disk_free - need_disk < _POLY_DISK_COMFORT_HEADROOM:
        return "tight"
    if ram_available is not None and ram_available < int(est_ram * 0.6):
        return "tight"
    if backend == "metal":
        if metal_budget is not None and est_ram > int(metal_budget * 0.85):
            return "tight"
        return "fits"
    if vram is not None and vram < min_vram + _PC_VRAM_COMFORT_HEADROOM:
        return "tight"
    if engine_core == "strata-poly":
        if vram is not None and (vram / GIB) < beside_gib + 3.0:
            return "tight"
        if poly_ram_need_gb is not None and (ram_total / GIB) < poly_ram_need_gb + 2.0:
            return "tight"
        return "fits"
    if ram_total < est_ram + _PC_RAM_COMFORT_HEADROOM:
        return "tight"
    return "fits"


def fallback_fit_level(
    *,
    ram_capacity: int,
    ram_available: int | None,
    min_ram_bytes: int,
    disk_free: int,
    min_disk_bytes: int,
) -> str:
    """Return 'tight' when a llama.cpp fallback model fits near memory or disk limits."""
    if ram_capacity < int(min_ram_bytes * 1.25):
        return "tight"
    if ram_available is not None and ram_available < int(min_ram_bytes * 0.6):
        return "tight"
    if disk_free - min_disk_bytes < _FALLBACK_DISK_COMFORT_HEADROOM:
        return "tight"
    return "fits"


def _pick_first(candidates: list[dict], predicate) -> dict | None:
    return next((item for item in candidates if predicate(item)), None)


def select_polystrata_alternatives(
    chosen: dict, remaining: list[dict], *, limit: int = 2
) -> list[dict]:
    """Pick diverse PolyStrata alternatives (balanced + lightweight) for flat card comparison."""
    if not remaining or limit <= 0:
        return []

    chosen_family = chosen.get("family")
    chosen_core = chosen.get("engine_core")
    chosen_dl = int(chosen.get("estimated_download_bytes") or 0)
    picked: list[dict] = []
    picked_ids: set[str] = set()

    def _add(item: dict | None) -> None:
        if item is not None and item["model_id"] not in picked_ids and len(picked) < limit:
            picked.append(item)
            picked_ids.add(item["model_id"])

    # Slot 1: balanced alternative (prefer distinct family, or a non-tight fit if chosen is tight).
    if chosen_core == "strata":
        balanced = (
            _pick_first(
                remaining,
                lambda x: x.get("engine_core") == "strata" and x.get("family") != chosen_family,
            )
            or _pick_first(remaining, lambda x: x.get("engine_core") == "strata")
            or _pick_first(remaining, lambda x: x.get("family") != chosen_family)
            or remaining[0]
        )
    elif chosen.get("fit_level") == "tight":
        balanced = (
            _pick_first(
                remaining,
                lambda x: x.get("fit_level") == "fits" and x.get("family") != chosen_family,
            )
            or _pick_first(remaining, lambda x: x.get("fit_level") == "fits")
            or _pick_first(remaining, lambda x: x.get("family") != chosen_family)
            or remaining[0]
        )
    else:
        balanced = (
            _pick_first(remaining, lambda x: x.get("family") != chosen_family) or remaining[0]
        )
    _add(balanced)

    # Slot 2: lightweight alternative (strata-poly on strata hosts, or clearly smaller download).
    if len(picked) < limit:
        pool = [item for item in remaining if item["model_id"] not in picked_ids]
        if chosen_core != "strata-poly":
            lightweight = _pick_first(pool, lambda x: x.get("engine_core") == "strata-poly")
        else:
            smaller = [
                item for item in pool if int(item.get("estimated_download_bytes") or 0) < chosen_dl
            ]
            if smaller:
                lightweight = min(
                    smaller,
                    key=lambda x: (
                        0 if x.get("fit_level") == "fits" else 1,
                        int(x.get("estimated_download_bytes") or 0),
                        -int(x.get("quality_rank") or 0),
                    ),
                )
            else:
                lightweight = None
        _add(lightweight)

    # Fill any remaining slots in quality order without duplicates.
    for item in remaining:
        if len(picked) >= limit:
            break
        _add(item)

    return picked


def assign_polystrata_tiers(chosen: dict, poly_alts: list[dict]) -> dict[str, str]:
    """Map selected model_ids to presentation tiers ('best' / 'balanced' / 'lightweight')."""
    tiers: dict[str, str] = {chosen["model_id"]: "best"}
    chosen_dl = int(chosen.get("estimated_download_bytes") or 0)
    chosen_core = chosen.get("engine_core")

    if len(poly_alts) == 1:
        only = poly_alts[0]
        only_dl = int(only.get("estimated_download_bytes") or 0)
        is_light = (chosen_core == "strata" and only.get("engine_core") == "strata-poly") or (
            chosen_dl > 0 and only_dl <= int(chosen_dl * 0.65)
        )
        tiers[only["model_id"]] = "lightweight" if is_light else "balanced"
        return tiers

    for idx, item in enumerate(poly_alts):
        if idx == 0:
            tiers[item["model_id"]] = "balanced"
        elif idx == 1:
            tiers[item["model_id"]] = "lightweight"
        else:
            tiers[item["model_id"]] = "balanced"
    return tiers
