"""Hardware-aware starter engine and model recommendation matrix tests."""

from __future__ import annotations

import json

from inferyard.cli import main
from inferyard.platforms import device_preflight
from inferyard.platforms.hardware_recommendation import GIB, STARTER_DEFINITION, recommend_starter


def _nvidia_gpu(name="RTX 4070", vram_gib=12, compute_cap="8.9", driver="580.42", index=0):
    return {
        "index": index,
        "vendor": "nvidia",
        "name": name,
        "driver_version": driver,
        "compute_capability": compute_cap,
        "memory_kind": "dedicated",
        "memory_total_bytes": vram_gib * GIB,
        "memory_free_bytes": max(1, vram_gib - 1) * GIB,
        "missing": {},
    }


def _base_hw(
    *,
    platform_name="Linux",
    ram_gib=32,
    avail_gib=24,
    disk_gib=150,
    devices=None,
    os_version="6.8",
    apple_silicon=None,
    avx2=True,
    page_file_gib=None,
):
    hw = {
        "platform": platform_name,
        "os_version": os_version,
        "architecture": "arm64" if apple_silicon else "x86_64",
        "cpu_model": "Test CPU",
        "cpu_features": {"avx2": avx2, "avx512": False},
        "logical_cpus": 16,
        "memory_total_bytes": ram_gib * GIB if ram_gib is not None else None,
        "memory_available_bytes": avail_gib * GIB if avail_gib is not None else None,
        "disk_free_bytes": disk_gib * GIB if disk_gib is not None else None,
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


def test_rtx_4070_8gb_32gb_recommends_polystrata_strata_poly():
    hw = _base_hw(
        platform_name="Windows",
        ram_gib=32,
        avail_gib=24,
        disk_gib=100,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 4070 Laptop GPU", vram_gib=8)],
        page_file_gib=32,
    )
    rec = recommend_starter(hw)
    assert rec["definition"] == STARTER_DEFINITION
    assert rec["status"] == "recommended"
    assert rec["engine"] == "polystrata"
    assert rec["backend"] == "cuda"
    assert rec["engine_core"] == "strata-poly"
    assert rec["family"] == "qwen36"
    assert rec["recommended_context"] == 32768
    assert rec["reason"] == "polystrata_hardware_fit"
    assert any(alt["engine"] == "llama-cpp" for alt in rec["alternatives"])


def test_rtx_5070_12gb_64gb_and_rtx_5090_32gb_120gb_recommend_qwen38():
    hw_5070 = _base_hw(
        ram_gib=64,
        avail_gib=56,
        disk_gib=150,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 5070", vram_gib=12, compute_cap="12.0")],
    )
    rec_5070 = recommend_starter(hw_5070)
    assert rec_5070["engine"] == "polystrata"
    assert rec_5070["family"] == "qwen"
    assert rec_5070["size"] == "IQ3_XXS"
    assert rec_5070["recommended_context"] == 32768

    hw_5090 = _base_hw(
        ram_gib=120,
        avail_gib=100,
        disk_gib=200,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 5090", vram_gib=32, compute_cap="12.0")],
    )
    rec_5090 = recommend_starter(hw_5090)
    assert rec_5090["engine"] == "polystrata"
    assert rec_5090["family"] == "qwen"
    assert rec_5090["size"] == "IQ3_S"
    assert rec_5090["recommended_context"] == 131072


def test_outdated_nvidia_driver_and_old_compute_cap_fall_back_to_llama_cpp():
    hw_old_drv = _base_hw(
        ram_gib=64,
        disk_gib=150,
        devices=[_nvidia_gpu("RTX 4090", vram_gib=24, compute_cap="8.9", driver="570.12")],
    )
    rec = recommend_starter(hw_old_drv)
    assert rec["status"] == "recommended"
    assert rec["engine"] == "llama-cpp"
    assert rec["backend"] == "cuda"
    assert "nvidia_driver_too_old" in rec["notes"]

    hw_pascal = _base_hw(
        ram_gib=64,
        disk_gib=150,
        devices=[_nvidia_gpu("GTX 1080 Ti", vram_gib=11, compute_cap="6.1", driver="580.42")],
    )
    rec_pascal = recommend_starter(hw_pascal)
    assert rec_pascal["engine"] == "llama-cpp"
    assert "nvidia_compute_capability_unsupported" in rec_pascal["notes"]

    # Mixed GTX 1080 + RTX 4070 picks RTX 4070 without spurious unsupported note
    hw_mixed = _base_hw(
        ram_gib=32,
        disk_gib=100,
        devices=[
            _nvidia_gpu("GTX 1080", vram_gib=8, compute_cap="6.1", driver="580.42", index=0),
            _nvidia_gpu("RTX 4070", vram_gib=12, compute_cap="8.9", driver="580.42", index=1),
        ],
    )
    rec_mixed = recommend_starter(hw_mixed)
    assert rec_mixed["engine"] == "polystrata"
    assert rec_mixed["gpu"]["name"] == "RTX 4070"
    assert "nvidia_compute_capability_unsupported" not in rec_mixed["notes"]


def test_amd_linux_recommends_hip_and_amd_windows_unverified_vram_falls_back():
    amd_linux = _base_hw(
        platform_name="Linux",
        ram_gib=64,
        avail_gib=54,
        disk_gib=150,
        devices=[
            {
                "index": 0,
                "vendor": "amd",
                "name": "AMD Radeon RX 7900 XTX",
                "arch_family": "gfx1100",
                "memory_kind": "dedicated",
                "memory_total_bytes": 24 * GIB,
                "memory_free_bytes": 22 * GIB,
                "missing": {},
            }
        ],
    )
    rec_linux = recommend_starter(amd_linux)
    assert rec_linux["engine"] == "polystrata"
    assert rec_linux["backend"] == "hip"
    assert rec_linux["recommended_context"] == 131072

    amd_win = _base_hw(
        platform_name="Windows",
        ram_gib=64,
        avail_gib=54,
        disk_gib=150,
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


def test_apple_silicon_16gb_32gb_64gb_and_macos_14_matrix():
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
    # 16 GB Mac -> llama-cpp Metal fallback
    rec_16 = recommend_starter(
        _base_hw(
            platform_name="Darwin",
            os_version="15.4",
            apple_silicon=True,
            ram_gib=16,
            avail_gib=12,
            disk_gib=100,
            devices=mac_gpu,
        )
    )
    assert rec_16["engine"] == "llama-cpp"
    assert rec_16["backend"] == "metal"
    assert rec_16["model_id"] == "model-qwen3-4b-q4km"
    assert rec_16["reason"] == "fallback_llama_cpp_insufficient_ram_for_polystrata"

    # 32 GB Mac -> PolyStrata Metal strata-poly (qwen36)
    rec_32 = recommend_starter(
        _base_hw(
            platform_name="Darwin",
            os_version="15.4",
            apple_silicon=True,
            ram_gib=32,
            avail_gib=24,
            disk_gib=80,
            devices=mac_gpu,
        )
    )
    assert rec_32["engine"] == "polystrata"
    assert rec_32["backend"] == "metal"
    assert rec_32["family"] == "qwen36"
    assert rec_32["recommended_context"] == 32768

    # 64 GB Mac -> PolyStrata Metal Qwen3.8 IQ3_S
    rec_64 = recommend_starter(
        _base_hw(
            platform_name="Darwin",
            os_version="15.4",
            apple_silicon=True,
            ram_gib=64,
            avail_gib=50,
            disk_gib=150,
            devices=mac_gpu,
        )
    )
    assert rec_64["engine"] == "polystrata"
    assert rec_64["backend"] == "metal"
    assert rec_64["family"] == "qwen"
    assert rec_64["size"] == "IQ3_S"
    assert rec_64["recommended_context"] == 131072

    # macOS 14.5 -> below Metal 15 requirement -> llama-cpp Metal fallback
    rec_14 = recommend_starter(
        _base_hw(
            platform_name="Darwin",
            os_version="14.5",
            apple_silicon=True,
            ram_gib=64,
            avail_gib=50,
            disk_gib=150,
            devices=mac_gpu,
        )
    )
    assert rec_14["engine"] == "llama-cpp"
    assert rec_14["backend"] == "metal"
    assert "macos_version_below_15" in rec_14["notes"]


def test_cpu_only_disk_limits_missing_fields_and_notes():
    # CPU-only
    rec_cpu = recommend_starter(_base_hw(ram_gib=16, avail_gib=12, disk_gib=50, devices=[]))
    assert rec_cpu["engine"] == "llama-cpp"
    assert rec_cpu["backend"] == "cpu"
    assert rec_cpu["model_id"] == "model-qwen3-4b-q4km"

    # Enough GPU/RAM for PolyStrata, but only 10 GiB disk -> fallback llama-cpp
    rec_low_disk = recommend_starter(
        _base_hw(ram_gib=32, disk_gib=10, devices=[_nvidia_gpu(vram_gib=8)])
    )
    assert rec_low_disk["engine"] == "llama-cpp"
    assert rec_low_disk["reason"] == "fallback_llama_cpp_insufficient_disk_for_polystrata"

    # Too little disk for any model -> blocked
    rec_no_disk = recommend_starter(_base_hw(ram_gib=16, disk_gib=0, devices=[]))
    assert rec_no_disk["status"] == "blocked"
    assert rec_no_disk["reason"] == "insufficient_disk_space"
    assert rec_no_disk["engine"] is None

    # Unmeasured disk -> blocked (never guess)
    rec_missing_disk = recommend_starter(_base_hw(ram_gib=32, disk_gib=None, devices=[]))
    assert rec_missing_disk["status"] == "blocked"
    assert rec_missing_disk["reason"] == "disk_space_unavailable"

    # Unmeasured memory -> blocked
    rec_missing_mem = recommend_starter(_base_hw(ram_gib=None, avail_gib=None, disk_gib=50))
    assert rec_missing_mem["status"] == "blocked"
    assert rec_missing_mem["reason"] == "host_memory_unavailable"

    # Non-AVX2 + low Windows page file notes + multi-root primary models disk
    hw_notes = _base_hw(
        platform_name="Windows",
        ram_gib=32,
        avail_gib=24,
        disk_gib=5,
        devices=[_nvidia_gpu(vram_gib=8)],
        avx2=False,
        page_file_gib=8,
    )
    hw_notes["models_disk_free_bytes"] = None
    hw_notes["models_disk_roots"] = [
        {"path": "D:\\models", "disk_free_bytes": 120 * GIB},
        {"path": "E:\\extra", "disk_free_bytes": 5 * GIB},
    ]
    hw_notes["missing"]["models_disk_free_bytes"] = "multiple_models_roots_unselected"
    rec_notes = recommend_starter(hw_notes)
    assert rec_notes["engine"] == "polystrata"
    assert "cpu_missing_avx2_experimental_build" in rec_notes["notes"]
    assert "windows_page_file_low" in rec_notes["notes"]
    assert "models_disk_from_primary_models_root" in rec_notes["notes"]


def test_device_check_cli_json_and_human_emit_starter_recommendation(monkeypatch, capsys):
    hw = _base_hw(
        platform_name="Linux",
        ram_gib=64,
        avail_gib=56,
        disk_gib=150,
        devices=[_nvidia_gpu("NVIDIA GeForce RTX 5070", vram_gib=12, compute_cap="12.0")],
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
    assert details["starter_recommendation"]["size"] == "IQ3_XXS"
    assert "recommendation" in details and "hardware" in details

    assert main(["device-check"]) == 0
    human_out = capsys.readouterr().out
    assert "默认推荐：" in human_out
    assert "PolyStrata" in human_out
    assert "Qwen3.8-Flash-Next (IQ3_XXS)" in human_out
