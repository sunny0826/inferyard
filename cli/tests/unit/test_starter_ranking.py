"""Tests for starter fit-level grading, tier assignment, and diverse alternatives."""

from __future__ import annotations

from inferyard.platforms.hardware_recommendation import GIB, recommend_starter
from tests.unit.test_hardware_recommendation import _base_hw, _nvidia_gpu


def _apple_mac(ram_gib: int, *, avail_gib: int | None = None, disk_gib: int = 200):
    hw = _base_hw(
        platform_name="Darwin",
        ram_gib=ram_gib,
        avail_gib=avail_gib if avail_gib is not None else int(ram_gib * 0.7),
        disk_bytes=disk_gib * GIB,
        os_version="15.4",
        apple_silicon=True,
        devices=[
            {
                "name": "Apple M4 Pro",
                "vendor": "apple",
                "arch_family": "Apple Silicon",
                "cores": 20,
                "metal_supported": True,
            }
        ],
    )
    hw["xcode_clt_installed"] = True
    return hw


def test_high_ram_pc_diversifies_alternatives_and_exposes_full_polystrata_models():
    hw = _base_hw(
        platform_name="Windows",
        ram_gib=120,
        avail_gib=96,
        disk_bytes=400 * GIB,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 5090", vram_mib=32607, compute_cap="12.0")],
        page_file_gib=32,
    )
    rec = recommend_starter(hw)
    assert rec["status"] == "recommended"
    assert rec["tier"] == "best"
    assert rec["fit_level"] == "fits"
    assert rec["model_id"] == "polystrata-qwen38-iq3-s"

    alts = rec["alternatives"]
    assert len(alts) == 3
    assert [alt["tier"] for alt in alts] == ["balanced", "lightweight", "fallback"]
    # Avoid stacking multiple same-family Qwen3.8 quants in flat alternatives
    assert alts[0]["family"] == "coder"
    assert alts[1]["engine_core"] == "strata-poly"
    assert alts[1]["model_id"] == "polystrata-qwen36-ud-q4-k-m"
    assert alts[2]["engine"] == "llama-cpp"

    # Full list still contains every hardware-compatible PolyStrata quant for the config drawer
    full_ids = {item["model_id"] for item in rec["polystrata_models"]}
    assert {
        "polystrata-qwen38-iq3-s",
        "polystrata-qwen38-iq3-xxs",
        "polystrata-qwen38-q2-0",
        "polystrata-qwen38-coder-iq1-m",
        "polystrata-qwen36-ud-q4-k-m",
    }.issubset(full_ids)


def test_mac_48gb_and_36gb_diversify_and_grade_metal_headroom():
    # On 48 GiB unified memory (~38.65 GB Metal budget), Coder IQ1_M (37 GB) fits tightly (>85%)
    # while Qwen3.6 Q4 (28.7 GB) is surfaced as a comfortable strata-poly alternative.
    rec_48 = recommend_starter(_apple_mac(48, avail_gib=34, disk_gib=200))
    assert rec_48["engine"] == "polystrata"
    assert rec_48["model_id"] == "polystrata-qwen38-coder-iq1-m"
    assert rec_48["fit_level"] == "tight"
    assert rec_48["tier"] == "best"
    poly_alts = [alt for alt in rec_48["alternatives"] if alt["engine"] == "polystrata"]
    assert any(
        alt["model_id"] == "polystrata-qwen36-ud-q4-k-m" and alt["fit_level"] == "fits"
        for alt in poly_alts
    )
    assert len(rec_48["polystrata_models"]) >= 5

    # On 36 GiB unified memory (~28.99 GB budget), Qwen3.6 Q4 (28.7 GB) is >85% budget -> tight,
    # and another family (lfm25) is surfaced with comfortable 'fits'.
    rec_36 = recommend_starter(_apple_mac(36, avail_gib=24, disk_gib=200))
    assert rec_36["engine"] == "polystrata"
    assert rec_36["model_id"] == "polystrata-qwen36-ud-q4-k-m"
    assert rec_36["fit_level"] == "tight"
    assert any(
        alt["engine"] == "polystrata"
        and alt["family"] != "qwen36"
        and alt["fit_level"] == "fits"
        for alt in rec_36["alternatives"]
    )


def test_pc_tight_fit_level_for_ram_tolerance_disk_headroom_and_fallback():
    # RTX 4070 8 GiB + 24 GiB RAM: Qwen3.6 Q4 needs ~26.7 GB (inside 4 GiB tolerance) -> tight
    hw_tight_ram = _base_hw(
        platform_name="Windows",
        ram_gib=24,
        avail_gib=18,
        disk_bytes=120 * GIB,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 4070 Laptop GPU", vram_mib=8188)],
        page_file_gib=24,
    )
    rec = recommend_starter(hw_tight_ram)
    assert rec["engine"] == "polystrata"
    assert rec["model_id"] == "polystrata-qwen36-ud-q4-k-m"
    assert rec["fit_level"] == "tight"

    # Tight disk headroom (< 10 GiB left after model + runtime) marks PolyStrata as tight
    hw_tight_disk = _apple_mac(48, avail_gib=36, disk_gib=30)
    rec_disk = recommend_starter(hw_tight_disk)
    assert rec_disk["engine"] == "polystrata"
    assert rec_disk["fit_level"] == "tight"

    # CPU-only fallback: tiers assigned and tight RAM flagged when total RAM < 1.25x min_ram
    hw_cpu_tight = _base_hw(ram_gib=7, avail_gib=5, disk_bytes=40 * GIB, devices=[])
    rec_cpu = recommend_starter(hw_cpu_tight)
    assert rec_cpu["engine"] == "llama-cpp"
    assert rec_cpu["tier"] == "best"
    assert rec_cpu["fit_level"] == "tight"
    assert [alt["tier"] for alt in rec_cpu["alternatives"]] == ["lightweight", "fallback"]
    assert all(alt["fit_level"] == "fits" for alt in rec_cpu["alternatives"])
