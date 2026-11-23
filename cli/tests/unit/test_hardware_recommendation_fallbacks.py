"""CPU fallback, host-limit notes, and CLI device-check starter recommendation tests."""

from __future__ import annotations

import json

from inferyard.cli import main
from inferyard.platforms import device_preflight
from inferyard.platforms.hardware_recommendation import GIB, recommend_starter
from tests.unit.test_hardware_recommendation import _base_hw, _nvidia_gpu


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
