"""Unit tests for Apple Silicon mlx-lm starter recommendations and catalog titles."""

from __future__ import annotations

from inferyard.platforms.hardware_recommendation import recommend_starter
from inferyard.platforms.polystrata_sizing import GIB, load_starter_catalog


def _apple_silicon_hw(*, ram_gib: int, disk_gib: int, os_version: str = "15.4") -> dict:
    return {
        "platform": "Darwin",
        "os_version": os_version,
        "apple_silicon": True,
        "xcode_clt_installed": True,
        "memory_total_bytes": ram_gib * GIB,
        "memory_available_bytes": int(ram_gib * 0.75 * GIB),
        "disk_free_bytes": disk_gib * GIB,
        "gpu": {
            "status": "observed",
            "devices": [{"name": "Apple M4 Pro", "vendor": "apple", "metal_supported": True}],
        },
    }


def test_apple_silicon_48g_includes_mlx_lm_alternative() -> None:
    rec = recommend_starter(_apple_silicon_hw(ram_gib=48, disk_gib=200))
    assert rec["status"] == "recommended"
    assert rec["engine"] == "polystrata"
    engines = [alt["engine"] for alt in rec["alternatives"]]
    assert "mlx-lm" in engines
    mlx_alt = next(alt for alt in rec["alternatives"] if alt["engine"] == "mlx-lm")
    assert mlx_alt["model_id"] == "model-qwen3-0.6b-mlx"
    assert mlx_alt["backend"] == "metal"
    assert mlx_alt["tier"] == "lightweight"


def test_apple_silicon_low_disk_promotes_mlx_lm_primary() -> None:
    rec = recommend_starter(_apple_silicon_hw(ram_gib=16, disk_gib=3))
    assert rec["status"] == "recommended"
    assert rec["engine"] == "mlx-lm"
    assert rec["model_id"] == "model-qwen3-0.6b-mlx"
    assert rec["backend"] == "metal"


def test_non_apple_host_excludes_mlx_lm() -> None:
    hw = {
        "platform": "Windows",
        "memory_total_bytes": 16 * GIB,
        "memory_available_bytes": 12 * GIB,
        "disk_free_bytes": 50 * GIB,
        "gpu": {"status": "none", "devices": []},
    }
    rec = recommend_starter(hw)
    assert rec["status"] == "recommended"
    assert rec["engine"] == "llama-cpp"
    assert all(alt["engine"] != "mlx-lm" for alt in rec["alternatives"])


def test_starter_catalog_fallback_titles_use_ascii_parentheses() -> None:
    cat = load_starter_catalog()
    for entry in cat["fallback_models"]:
        assert "（" not in entry["title"]
        assert "）" not in entry["title"]
