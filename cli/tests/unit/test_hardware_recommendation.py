"""Hardware-aware starter engine and model recommendation matrix tests."""

from __future__ import annotations

import json

from inferyard.cli import main
from inferyard.platforms import device_gpu, device_preflight
from inferyard.platforms.hardware_recommendation import (
    GIB,
    STARTER_DEFINITION,
    _load_catalog,
    recommend_starter,
)


def _nvidia_gpu(
    name="RTX 4070",
    vram_mib=12288,
    compute_cap="8.9",
    driver="580.42",
    index=0,
):
    total = vram_mib * 1024**2
    return {
        "index": index,
        "vendor": "nvidia",
        "name": name,
        "driver_version": driver,
        "compute_capability": compute_cap,
        "memory_kind": "dedicated",
        "memory_total_bytes": total,
        "memory_free_bytes": max(1, total - GIB),
        "missing": {},
    }


def _base_hw(
    *,
    platform_name="Linux",
    ram_gib=32,
    avail_gib=24,
    disk_bytes=150 * GIB,
    devices=None,
    os_version="6.8",
    apple_silicon=None,
    avx2=True,
    avx512=False,
    page_file_gib=None,
):
    hw = {
        "platform": platform_name,
        "os_version": os_version,
        "architecture": "arm64" if apple_silicon else "x86_64",
        "cpu_model": "Test CPU",
        "cpu_features": {"avx2": avx2, "avx512": avx512},
        "logical_cpus": 16,
        "memory_total_bytes": ram_gib * GIB if ram_gib is not None else None,
        "memory_available_bytes": avail_gib * GIB if avail_gib is not None else None,
        "disk_free_bytes": disk_bytes,
        "disk_path": "/workspace",
        "gpu": {
            "status": "observed" if devices else "unavailable",
            "reason": None if devices else "nvidia_smi_not_found",
            "devices": devices or [],
        },
        "sources": {},
        "missing": {},
    }
    if apple_silicon is not None:
        hw["apple_silicon"] = apple_silicon
    if page_file_gib is not None:
        hw["page_file_total_bytes"] = page_file_gib * GIB
    return hw


def test_catalog_aligns_with_polystrata_release_sources():
    cat = _load_catalog()
    by_id = {m["id"]: m for m in cat["polystrata_models"]}
    q36_q4 = by_id["polystrata-qwen36-ud-q4-k-m"]
    assert q36_q4["gguf_sources"]["huggingface_repo"] == "unsloth/Qwen3.6-35B-A3B-MTP-GGUF"
    assert q36_q4["download_bytes"] == 22663387424
    assert q36_q4["gguf_sources"]["sha256"] == (
        "0b21525e972670ed59e1812e170b27c26355381f0656ecc4e25617ece7dac58b"
    )
    flint_q4 = by_id["polystrata-flint-ud-q4-k-xl"]
    assert flint_q4["gguf_sources"]["huggingface_repo"] == "unsloth/flint-GGUF"
    assert flint_q4["gguf_sources"]["filename"] == "flint-UD-Q4_K_XL.gguf"
    assert flint_q4["download_bytes"] == 25054688704


def test_real_nvidia_smi_mib_readings_for_rtx_4070_laptop_and_rtx_5070():
    # Real nvidia-smi MiB readings (slightly below nominal 8 / 12 GiB due to driver reserve)
    dev_4070 = device_gpu._parse_nvidia_rows(
        "0, NVIDIA GeForce RTX 4070 Laptop GPU, 580.42, 8188, 7600, 5, 40, 15.0, 8.9\n",
        expect_compute_cap=True,
    )
    hw_4070 = _base_hw(
        platform_name="Windows",
        ram_gib=32,
        avail_gib=24,
        disk_bytes=100 * GIB,
        devices=dev_4070,
        page_file_gib=32,
    )
    rec_4070 = recommend_starter(hw_4070)
    assert rec_4070["definition"] == STARTER_DEFINITION
    assert rec_4070["status"] == "recommended"
    assert rec_4070["engine"] == "polystrata"
    assert rec_4070["backend"] == "cuda"
    assert rec_4070["engine_core"] == "strata-poly"
    assert rec_4070["family"] == "qwen36"
    assert rec_4070["gpu"]["vram_bytes"] == 8188 * 1024**2
    assert rec_4070["recommended_context"] == 32768
    assert any(alt["engine"] == "llama-cpp" for alt in rec_4070["alternatives"])

    dev_5070 = device_gpu._parse_nvidia_rows(
        "0, NVIDIA GeForce RTX 5070, 580.42, 12227, 11800, 3, 38, 12.0, 12.0\n",
        expect_compute_cap=True,
    )
    hw_5070 = _base_hw(
        ram_gib=64,
        avail_gib=56,
        disk_bytes=150 * GIB,
        devices=dev_5070,
    )
    rec_5070 = recommend_starter(hw_5070)
    assert rec_5070["engine"] == "polystrata"
    assert rec_5070["engine_core"] == "strata"
    assert rec_5070["family"] == "qwen"
    assert rec_5070["size"] == "IQ3_S"
    assert rec_5070["gpu"]["vram_bytes"] == 12227 * 1024**2
    assert rec_5070["recommended_context"] == 32768


def test_rtx_5090_32gb_120gb_and_avx512_q2_0_disk_budget():
    hw_5090 = _base_hw(
        ram_gib=120,
        avail_gib=100,
        disk_bytes=200 * GIB,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 5090", vram_mib=32607, compute_cap="12.0")],
    )
    rec_5090 = recommend_starter(hw_5090)
    assert rec_5090["engine"] == "polystrata"
    assert rec_5090["family"] == "qwen"
    assert rec_5090["size"] == "IQ3_S"
    assert rec_5090["recommended_context"] == 131072

    # 12 GiB CUDA, 48 GiB RAM, AVX-512 CPU, 105 GB free disk:
    # Q2_0 needs 66.4 GB + 8 GB MTP + 40 GB AVX-512 conversion (114.4 GB) -> must NOT pick Q2_0!
    hw_avx512 = _base_hw(
        ram_gib=48,
        avail_gib=40,
        disk_bytes=105_000_000_000,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 5070", vram_mib=12227, compute_cap="12.0")],
        avx512=True,
    )
    rec_avx512 = recommend_starter(hw_avx512)
    assert rec_avx512["engine"] == "polystrata"
    assert rec_avx512["size"] != "Q2_0"
    assert rec_avx512["model_id"] == "polystrata-qwen38-coder-iq1-m"

    # 12 GiB CUDA, 32 GiB RAM, 75 GB free disk:
    # Coder IQ1_M enters low-RAM mode (58.4 + 8 + 24.4 = 90.8 GB > 75 GB) -> must NOT pick IQ1_M!
    hw_low_ram_disk = _base_hw(
        ram_gib=32,
        avail_gib=26,
        disk_bytes=75_000_000_000,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 5070", vram_mib=12227, compute_cap="12.0")],
    )
    rec_low_ram_disk = recommend_starter(hw_low_ram_disk)
    assert rec_low_ram_disk["engine"] == "polystrata"
    assert rec_low_ram_disk["model_id"] != "polystrata-qwen38-coder-iq1-m"
    assert rec_low_ram_disk["family"] == "qwen36"

    # 16 GiB RAM + RTX 4070 Laptop 8188 MiB:
    # Matches upstream poly_fits(): Qwen3.6 UD-Q3_K_M is rejected; Gemma 4 UD-Q3_K_M & Qwen3.6 UD-Q2_K_XL fit.
    hw_16gb_4070 = _base_hw(
        platform_name="Windows",
        ram_gib=16,
        avail_gib=12,
        disk_bytes=100 * GIB,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 4070 Laptop GPU", vram_mib=8188)],
    )
    rec_16gb_4070 = recommend_starter(hw_16gb_4070)
    assert rec_16gb_4070["engine"] == "polystrata"
    assert rec_16gb_4070["model_id"] == "polystrata-gemma4-ud-q3-k-m"
    all_ids = [rec_16gb_4070["model_id"], *(alt["model_id"] for alt in rec_16gb_4070["alternatives"])]
    assert "polystrata-qwen36-ud-q3-k-m" not in all_ids
    assert "polystrata-qwen36-ud-q2-k-xl" in all_ids


def test_outdated_nvidia_driver_and_old_compute_cap_fall_back_to_llama_cpp():
    hw_old_drv = _base_hw(
        ram_gib=64,
        disk_bytes=150 * GIB,
        devices=[_nvidia_gpu("RTX 4090", vram_mib=24564, compute_cap="8.9", driver="570.12")],
    )
    rec = recommend_starter(hw_old_drv)
    assert rec["status"] == "recommended"
    assert rec["engine"] == "llama-cpp"
    assert rec["backend"] == "cuda"
    assert "nvidia_driver_too_old" in rec["notes"]

    hw_pascal = _base_hw(
        ram_gib=64,
        disk_bytes=150 * GIB,
        devices=[_nvidia_gpu("GTX 1080 Ti", vram_mib=11264, compute_cap="6.1", driver="580.42")],
    )
    rec_pascal = recommend_starter(hw_pascal)
    assert rec_pascal["engine"] == "llama-cpp"
    assert "nvidia_compute_capability_unsupported" in rec_pascal["notes"]

    # Mixed GTX 1080 + RTX 4070 picks RTX 4070 without spurious unsupported note
    hw_mixed = _base_hw(
        ram_gib=32,
        disk_bytes=100 * GIB,
        devices=[
            _nvidia_gpu("GTX 1080", vram_mib=8192, compute_cap="6.1", driver="580.42", index=0),
            _nvidia_gpu("RTX 4070", vram_mib=12227, compute_cap="8.9", driver="580.42", index=1),
        ],
    )
    rec_mixed = recommend_starter(hw_mixed)
    assert rec_mixed["engine"] == "polystrata"
    assert rec_mixed["gpu"]["name"] == "RTX 4070"
    assert "nvidia_compute_capability_unsupported" not in rec_mixed["notes"]


def test_amd_linux_windows_and_hybrid_nvidia_amd_backend_selection():
    amd_dev = {
        "index": 0,
        "vendor": "amd",
        "name": "AMD Radeon RX 7900 XTX",
        "arch_family": "gfx1100",
        "memory_kind": "dedicated",
        "memory_total_bytes": 24 * GIB,
        "memory_free_bytes": 22 * GIB,
        "missing": {},
    }
    rec_linux = recommend_starter(
        _base_hw(platform_name="Linux", ram_gib=64, avail_gib=54, disk_bytes=150 * GIB, devices=[amd_dev])
    )
    assert rec_linux["engine"] == "polystrata"
    assert rec_linux["backend"] == "hip"
    assert rec_linux["recommended_context"] == 131072

    # Hybrid host: 24 GiB AMD + 8 GiB NVIDIA on 15 GB disk -> CUDA LFM2.5 fits even though HIP does not
    hybrid_hw = _base_hw(
        platform_name="Linux",
        ram_gib=32,
        avail_gib=24,
        disk_bytes=15_000_000_000,
        devices=[amd_dev, _nvidia_gpu("RTX 4070 Laptop", vram_mib=8188, index=1)],
    )
    rec_hybrid = recommend_starter(hybrid_hw)
    assert rec_hybrid["engine"] == "polystrata"
    assert rec_hybrid["backend"] == "cuda"
    assert rec_hybrid["family"] == "lfm25"

    # AMD Linux with only 10 GiB disk -> CPU llama-cpp fallback must report gpu=None
    rec_amd_fallback = recommend_starter(
        _base_hw(
            platform_name="Linux",
            ram_gib=64,
            avail_gib=54,
            disk_bytes=10 * GIB,
            devices=[amd_dev],
        )
    )
    assert rec_amd_fallback["engine"] == "llama-cpp"
    assert rec_amd_fallback["backend"] == "cpu"
    assert rec_amd_fallback["gpu"] is None

    amd_win = _base_hw(
        platform_name="Windows",
        ram_gib=64,
        avail_gib=54,
        disk_bytes=150 * GIB,
        devices=[
            {
                "index": 0,
                "vendor": "amd",
                "name": "AMD Radeon RX 9070 XT",
                "arch_family": "gfx1201",
                "memory_kind": "unmeasured",
                "memory_total_bytes": None,
                "memory_free_bytes": None,
                "missing": {"memory_total_bytes": "windows_wmi_32bit_cap"},
            }
        ],
    )
    rec_win = recommend_starter(amd_win)
    assert rec_win["engine"] == "llama-cpp"
    assert "amd_vram_unverified" in rec_win["notes"]


def test_apple_silicon_metal_working_set_budget_and_macos_14():
    mac_gpu = [
        {
            "vendor": "apple",
            "name": "Apple M4",
            "metal_supported": True,
            "memory_kind": "shared_host",
            "memory_total_bytes": None,
            "memory_free_bytes": None,
        }
    ]
    # 16 GiB Mac -> Metal budget ~12.88 GB -> llama-cpp Metal fallback
    rec_16 = recommend_starter(
        _base_hw(
            platform_name="Darwin",
            os_version="15.4",
            apple_silicon=True,
            ram_gib=16,
            avail_gib=12,
            disk_bytes=100 * GIB,
            devices=mac_gpu,
        )
    )
    assert rec_16["engine"] == "llama-cpp"
    assert rec_16["backend"] == "metal"
    assert rec_16["model_id"] == "model-qwen3-4b-q4km"
    assert rec_16["reason"] == "fallback_llama_cpp_insufficient_ram_for_polystrata"

    # 32 GiB Mac -> Metal budget ~25.77 GB -> PolyStrata Metal Qwen3.6 UD-Q3_K_M (23.1 GB)
    rec_32 = recommend_starter(
        _base_hw(
            platform_name="Darwin",
            os_version="15.4",
            apple_silicon=True,
            ram_gib=32,
            avail_gib=24,
            disk_bytes=80 * GIB,
            devices=mac_gpu,
        )
    )
    assert rec_32["engine"] == "polystrata"
    assert rec_32["backend"] == "metal"
    assert rec_32["family"] == "qwen36"
    assert rec_32["size"] == "UD-Q3_K_M"
    assert rec_32["recommended_context"] == 32768

    # 64 GiB Mac -> Metal budget 48.10 GB -> Q2_0 (45.6 GB) stays resident, context 32768
    rec_64 = recommend_starter(
        _base_hw(
            platform_name="Darwin",
            os_version="15.4",
            apple_silicon=True,
            ram_gib=64,
            avail_gib=50,
            disk_bytes=150 * GIB,
            devices=mac_gpu,
        )
    )
    assert rec_64["engine"] == "polystrata"
    assert rec_64["backend"] == "metal"
    assert rec_64["family"] == "qwen"
    assert rec_64["size"] == "Q2_0"
    assert rec_64["recommended_context"] == 32768

    # 96 GiB Mac with 105 GB free -> 2-shard IQ3_S maps directly without repacking (~91.6 GB <= 105 GB)
    rec_96 = recommend_starter(
        _base_hw(
            platform_name="Darwin",
            os_version="15.4",
            apple_silicon=True,
            ram_gib=96,
            avail_gib=80,
            disk_bytes=105_000_000_000,
            devices=mac_gpu,
        )
    )
    assert rec_96["engine"] == "polystrata"
    assert rec_96["backend"] == "metal"
    assert rec_96["family"] == "qwen"
    assert rec_96["size"] == "IQ3_S"
    assert rec_96["recommended_context"] == 65536

    # macOS 14.5 -> below Metal 15 requirement -> llama-cpp Metal fallback
    rec_14 = recommend_starter(
        _base_hw(
            platform_name="Darwin",
            os_version="14.5",
            apple_silicon=True,
            ram_gib=64,
            avail_gib=50,
            disk_bytes=150 * GIB,
            devices=mac_gpu,
        )
    )
    assert rec_14["engine"] == "llama-cpp"
    assert rec_14["backend"] == "metal"
    assert "macos_version_below_15" in rec_14["notes"]


def test_cpu_only_disk_limits_order_invariance_and_tight_memory_notes():
    # CPU-only with tight available RAM (16 GiB total, 512 MiB free) flags host_available_memory_tight
    hw_tight = _base_hw(ram_gib=16, avail_gib=0, disk_bytes=50 * GIB, devices=[])
    hw_tight["memory_available_bytes"] = 512 * 1024**2
    rec_cpu = recommend_starter(hw_tight)
    assert rec_cpu["engine"] == "llama-cpp"
    assert rec_cpu["backend"] == "cpu"
    assert rec_cpu["model_id"] == "model-qwen3-4b-q4km"
    assert "host_available_memory_tight" in rec_cpu["notes"]

    # Enough GPU/RAM for PolyStrata, but only 10 GiB disk -> fallback llama-cpp
    rec_low_disk = recommend_starter(
        _base_hw(ram_gib=32, disk_bytes=10 * GIB, devices=[_nvidia_gpu(vram_mib=8188)])
    )
    assert rec_low_disk["engine"] == "llama-cpp"
    assert rec_low_disk["reason"] == "fallback_llama_cpp_insufficient_disk_for_polystrata"

    # Too little disk for any model -> blocked
    rec_no_disk = recommend_starter(_base_hw(ram_gib=16, disk_bytes=0, devices=[]))
    assert rec_no_disk["status"] == "blocked"
    assert rec_no_disk["reason"] == "insufficient_disk_space"
    assert rec_no_disk["engine"] is None

    # Unmeasured disk -> blocked (never guess)
    rec_missing_disk = recommend_starter(_base_hw(ram_gib=32, disk_bytes=None, devices=[]))
    assert rec_missing_disk["status"] == "blocked"
    assert rec_missing_disk["reason"] == "disk_space_unavailable"

    # Unmeasured memory -> blocked
    rec_missing_mem = recommend_starter(_base_hw(ram_gib=None, avail_gib=None, disk_bytes=50 * GIB))
    assert rec_missing_mem["status"] == "blocked"
    assert rec_missing_mem["reason"] == "host_memory_unavailable"

    # Multi-root unselected models disk must be order-invariant and stay blocked
    for roots in (
        [{"path": "/a", "disk_free_bytes": 120 * GIB}, {"path": "/b", "disk_free_bytes": 0}],
        [{"path": "/b", "disk_free_bytes": 0}, {"path": "/a", "disk_free_bytes": 120 * GIB}],
    ):
        hw_multi = _base_hw(ram_gib=32, disk_bytes=120 * GIB, devices=[_nvidia_gpu(vram_mib=8188)])
        hw_multi["models_disk_path"] = None
        hw_multi["models_disk_free_bytes"] = None
        hw_multi["models_disk_roots"] = roots
        hw_multi["missing"]["models_disk_path"] = "multiple_models_roots_unselected"
        hw_multi["missing"]["models_disk_free_bytes"] = "multiple_models_roots_unselected"
        rec_multi = recommend_starter(hw_multi)
        assert rec_multi["status"] == "blocked"
        assert rec_multi["reason"] == "disk_space_unavailable"
        assert "multiple_models_roots_unselected" in rec_multi["notes"]

    # Non-AVX2 + low Windows page file notes on PolyStrata recommendation
    hw_notes = _base_hw(
        platform_name="Windows",
        ram_gib=32,
        avail_gib=24,
        disk_bytes=100 * GIB,
        devices=[_nvidia_gpu(vram_mib=8188)],
        avx2=False,
        page_file_gib=8,
    )
    rec_notes = recommend_starter(hw_notes)
    assert rec_notes["engine"] == "polystrata"
    assert "cpu_missing_avx2_experimental_build" in rec_notes["notes"]
    assert "windows_page_file_low" in rec_notes["notes"]


def test_device_check_cli_json_and_human_emit_starter_recommendation(monkeypatch, capsys):
    hw = _base_hw(
        platform_name="Linux",
        ram_gib=64,
        avail_gib=56,
        disk_bytes=150 * GIB,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 5070", vram_mib=12227, compute_cap="12.0")],
    )
    hw["models_disk_path"] = "/models"
    hw["models_disk_free_bytes"] = 150 * GIB
    monkeypatch.setattr(device_preflight, "hardware_snapshot", lambda _p: dict(hw))

    assert main(["device-check", "--json"]) == 0
    payload = json.loads(capsys.readouterr().out)
    details = payload["details"]
    assert details["kind"] == "device_preflight.v1"
    assert details["starter_recommendation"]["engine"] == "polystrata"
    assert details["starter_recommendation"]["family"] == "qwen"
    assert details["starter_recommendation"]["size"] == "IQ3_S"
    assert "recommendation" in details and "hardware" in details

    assert main(["device-check"]) == 0
    human_out = capsys.readouterr().out
    assert "默认推荐：" in human_out
    assert "PolyStrata" in human_out
    assert "Qwen3.8-Flash-Next (IQ3_S)" in human_out
