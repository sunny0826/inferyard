"""Shared PolyStrata disk-requirement calculations for recommendation and preflight."""

from __future__ import annotations

from functools import lru_cache
from importlib.resources import files
from pathlib import Path

from inferyard.contracts.validation import strict_json_loads

GIB = 1024**3
MIB = 1024**2
DEFAULT_RUNTIME_DISK_BYTES = 2 * GIB
RESUME_RUNTIME_DISK_BYTES = 256 * MIB
GGUF_WORK_DISK_BYTES = 512 * MIB
STRATA_MTP_DISK_BYTES = 8_000_000_000
MAC_PACK_HEADROOM_BYTES = 6 * GIB
GGUF_HEADROOM_BYTES = 2 * GIB
_FALLBACK_EMPTY_CATALOG_BYTES = 24 * GIB


@lru_cache(maxsize=1)
def load_starter_catalog() -> dict:
    raw = files("inferyard").joinpath("data", "starter_catalog.json").read_text(encoding="utf-8")
    return strict_json_loads(raw)


def catalog_models_for_platform(
    *,
    system: str,
    backend: str | None = None,
    catalog: dict | None = None,
) -> list[dict]:
    cat = catalog if catalog is not None else load_starter_catalog()
    models = cat.get("polystrata_models") or []
    filtered = [entry for entry in models if system in (entry.get("platforms") or {})]
    if backend in ("cuda", "hip", "metal"):
        filtered = [
            entry
            for entry in filtered
            if backend in ((entry.get("platforms") or {}).get(system) or [])
        ]
    return filtered


def polystrata_model_disk_bytes(
    entry: dict,
    *,
    system: str,
    gguf_path: Path | None = None,
    avx512: bool | None = None,
) -> int:
    """Return required model-directory bytes aligned across recommendation and preflight."""
    if gguf_path is not None:
        if entry.get("model_format") == "strata_pack":
            pack = int(
                (entry.get("mac_pack_bytes") if system == "Darwin" else entry.get("arena_bytes"))
                or 0
            )
            if system != "Darwin" and avx512 is True:
                pack = max(pack, int(entry.get("avx512_conversion_bytes") or 0))
            return max(GGUF_WORK_DISK_BYTES, pack + GGUF_HEADROOM_BYTES)
        return GGUF_WORK_DISK_BYTES
    if "min_disk_bytes" in entry:
        return int(entry["min_disk_bytes"])
    download = int(entry.get("download_bytes") or 0)
    if entry.get("model_format") == "strata_pack":
        if system == "Darwin":
            pack = int(entry.get("mac_pack_bytes") or 0)
            return download + pack + MAC_PACK_HEADROOM_BYTES
        arena = int(entry.get("arena_bytes") or 0)
        if avx512 is True:
            arena = max(arena, int(entry.get("avx512_conversion_bytes") or 0))
        return download + arena + STRATA_MTP_DISK_BYTES
    draft = int((entry.get("gguf_sources") or {}).get("draft_bytes") or 0)
    return download + draft + GGUF_HEADROOM_BYTES


def polystrata_total_disk_bytes(
    entry: dict,
    *,
    system: str,
    gguf_path: Path | None = None,
    avx512: bool | None = None,
    runtime_bytes: int = DEFAULT_RUNTIME_DISK_BYTES,
) -> int:
    """Return total required disk bytes (model data + runtime extraction/venv overhead)."""
    return (
        polystrata_model_disk_bytes(
            entry, system=system, gguf_path=gguf_path, avx512=avx512
        )
        + runtime_bytes
    )


def _platform_entries_or_all(*, system: str, backend: str | None, catalog: dict | None) -> list[dict]:
    entries = catalog_models_for_platform(system=system, backend=backend, catalog=catalog)
    if entries:
        return entries
    cat = catalog if catalog is not None else load_starter_catalog()
    return list(cat.get("polystrata_models") or [])


def polystrata_min_model_disk_bytes(
    *,
    system: str,
    backend: str | None = None,
    catalog: dict | None = None,
) -> int:
    """Minimum catalog model disk requirement for the target platform/backend."""
    entries = _platform_entries_or_all(system=system, backend=backend, catalog=catalog)
    if not entries:
        return _FALLBACK_EMPTY_CATALOG_BYTES
    return min(polystrata_model_disk_bytes(entry, system=system) for entry in entries)


def polystrata_fallback_model_disk_bytes(
    *,
    system: str,
    backend: str | None = None,
    avx512: bool | None = None,
    catalog: dict | None = None,
) -> int:
    """Conservative fallback disk budget covering all platform defaults when model is omitted."""
    entries = _platform_entries_or_all(system=system, backend=backend, catalog=catalog)
    if not entries:
        return _FALLBACK_EMPTY_CATALOG_BYTES
    return max(
        polystrata_model_disk_bytes(entry, system=system, avx512=avx512) for entry in entries
    )
