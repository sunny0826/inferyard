"""Tests for shared PolyStrata disk sizing, alternative tiering, and Xcode CLT hints."""

from __future__ import annotations

from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.platforms import device_macos, device_preflight
from inferyard.platforms.hardware_recommendation import recommend_starter
from inferyard.platforms.polystrata_sizing import (
    GIB,
    load_starter_catalog,
    polystrata_total_disk_bytes,
)
from inferyard.provisioning import polystrata_preflight
from inferyard.provisioning.polystrata_preflight import run_polystrata_preflight
from inferyard.provisioning.polystrata_preflight_checks import _disk_free_bytes as _real_disk_free
from inferyard.provisioning.preparation_io import PreparationError
from polystrata_test_helpers import _disk_usage


def _mac_hw(
    *,
    ram_gib=96,
    disk_bytes=200 * GIB,
    models_disk_bytes: int | None = None,
    xcode_clt_installed=True,
):
    hw = {
        "platform": "Darwin",
        "os_version": "15.4",
        "architecture": "arm64",
        "apple_silicon": True,
        "xcode_clt_installed": xcode_clt_installed,
        "cpu_model": "Apple M4 Max",
        "cpu_features": {"avx2": False, "avx512": False},
        "logical_cpus": 16,
        "memory_total_bytes": ram_gib * GIB,
        "memory_available_bytes": int(ram_gib * 0.8 * GIB),
        "disk_free_bytes": disk_bytes,
        "disk_path": "/workspace",
        "gpu": {
            "status": "observed",
            "devices": [
                {
                    "vendor": "apple",
                    "name": "Apple M4 Max",
                    "metal_supported": True,
                    "memory_kind": "shared_host",
                    "memory_total_bytes": None,
                    "memory_free_bytes": None,
                }
            ],
        },
        "sources": {},
        "missing": {},
    }
    if models_disk_bytes is not None:
        hw["models_disk_free_bytes"] = models_disk_bytes
    return hw


def _pc_hw(*, platform_name="Linux", backend="cuda", ram_gib=64, disk_bytes=200 * GIB, avx512=False):
    if backend == "hip":
        device = {
            "index": 0,
            "vendor": "amd",
            "name": "AMD Radeon RX 7900 XTX",
            "arch_family": "gfx1100",
            "memory_kind": "dedicated",
            "memory_total_bytes": 24 * GIB,
            "memory_free_bytes": 22 * GIB,
            "missing": {},
        }
    else:
        device = {
            "index": 0,
            "vendor": "nvidia",
            "name": "RTX 5090",
            "driver_version": "580.42",
            "compute_capability": "12.0",
            "memory_kind": "dedicated",
            "memory_total_bytes": 32 * GIB,
            "memory_free_bytes": 30 * GIB,
            "missing": {},
        }
    return {
        "platform": platform_name,
        "os_version": "6.8" if platform_name == "Linux" else "11",
        "architecture": "x86_64",
        "cpu_model": "Test CPU",
        "cpu_features": {"avx2": True, "avx512": avx512},
        "logical_cpus": 16,
        "memory_total_bytes": ram_gib * GIB,
        "memory_available_bytes": int(ram_gib * 0.8 * GIB),
        "disk_free_bytes": disk_bytes,
        "disk_path": "/workspace",
        "gpu": {"status": "observed", "devices": [device]},
        "sources": {},
        "missing": {},
    }


def _preflight_for_option(
    tmp_path: Path,
    name: str,
    *,
    system: str,
    backend: str,
    family: str,
    model: str,
    disk_bytes: int,
    avx512: bool | None = None,
) -> dict:
    arch = "arm64" if system == "Darwin" else "x64"
    gpu_devices = (
        [{"vendor": "nvidia", "driver_version": "580.42"}]
        if backend == "cuda"
        else [{"vendor": "amd"}]
        if backend == "hip"
        else []
    )
    return run_polystrata_preflight(
        tmp_path / f"out-{name}",
        tmp_path / f"data-{name}",
        family=family,
        model=model,
        backend=backend,
        platform_info=(system, arch),
        macos_version="15.4" if system == "Darwin" else None,
        xcode_clt_check=(lambda: True) if system == "Darwin" else None,
        linux_venv_check=(lambda _py: True) if system == "Linux" else None,
        gpu_probe=(lambda _sys: {"status": "available", "devices": gpu_devices})
        if system != "Darwin"
        else None,
        build_tools_check=(lambda _nvcc: []) if system == "Linux" else None,
        cpu_features_probe=(lambda _sys: ({"avx512": avx512}, "test", {}))
        if system != "Darwin"
        else None,
        disk_usage_fn=_disk_usage(disk_bytes),
    )


def test_catalog_matrix_parity_between_recommendation_and_preflight(tmp_path: Path):
    catalog = load_starter_catalog()
    for idx, entry in enumerate(catalog["polystrata_models"]):
        single_cat = {"polystrata_models": [entry], "fallback_models": []}
        for system, backends in entry["platforms"].items():
            for backend in backends:
                for avx512 in (False, True) if system != "Darwin" else (False,):
                    required = polystrata_total_disk_bytes(entry, system=system, avx512=avx512)
                    hw = (
                        _mac_hw(ram_gib=128, disk_bytes=required)
                        if system == "Darwin"
                        else _pc_hw(
                            platform_name=system,
                            backend=backend,
                            ram_gib=128,
                            disk_bytes=required,
                            avx512=avx512,
                        )
                    )
                    rec = recommend_starter(hw, catalog=single_cat)
                    assert rec["status"] == "recommended"
                    assert rec["model_id"] == entry["id"]
                    report = _preflight_for_option(
                        tmp_path,
                        f"{idx}-{system}-{backend}-{avx512}-ok",
                        system=system,
                        backend=backend,
                        family=entry["family"],
                        model=entry["size"],
                        disk_bytes=required,
                        avx512=avx512,
                    )
                    assert report["blockers"] == []
                    assert report["runtime_required_bytes"] + report["model_required_bytes"] == required

                    low_disk = required - 1
                    hw_low = (
                        _mac_hw(ram_gib=128, disk_bytes=low_disk)
                        if system == "Darwin"
                        else _pc_hw(
                            platform_name=system,
                            backend=backend,
                            ram_gib=128,
                            disk_bytes=low_disk,
                            avx512=avx512,
                        )
                    )
                    assert recommend_starter(hw_low, catalog=single_cat)["status"] == "blocked"
                    with pytest.raises(PreparationError) as exc_low:
                        _preflight_for_option(
                            tmp_path,
                            f"{idx}-{system}-{backend}-{avx512}-low",
                            system=system,
                            backend=backend,
                            family=entry["family"],
                            model=entry["size"],
                            disk_bytes=low_disk,
                            avx512=avx512,
                        )
                    assert exc_low.value.reason == "polystrata_disk_insufficient"


def test_issue_179_mac_100gib_reproducer_and_avx512_q2_0_parity(tmp_path: Path):
    # Issue #179 reproducer: 96 GiB Mac with 100 GiB free must never pick IQ3_S,
    # and every recommended option passes preflight at 100 GiB.
    rec = recommend_starter(_mac_hw(ram_gib=96, disk_bytes=100 * GIB))
    assert rec["engine"] == "polystrata"
    assert rec["size"] != "IQ3_S"
    for item in [rec, *(a for a in rec["alternatives"] if a["engine"] == "polystrata")]:
        report = _preflight_for_option(
            tmp_path,
            f"mac100-{item['model_id']}",
            system="Darwin",
            backend=item["backend"],
            family=item["family"],
            model=item["size"],
            disk_bytes=100 * GIB,
        )
        assert report["blockers"] == []

    # AVX-512 adds conversion budget on Q2_0 across both recommendation and preflight
    disk = 112_000_000_000
    rec_no_avx = recommend_starter(_pc_hw(ram_gib=48, disk_bytes=disk, avx512=False))
    rec_avx = recommend_starter(_pc_hw(ram_gib=48, disk_bytes=disk, avx512=True))
    assert rec_no_avx["size"] == "Q2_0"
    assert rec_avx["model_id"] == "polystrata-qwen38-coder-iq1-m"
    assert (
        _preflight_for_option(
            tmp_path,
            "avx-no",
            system="Linux",
            backend="cuda",
            family=rec_no_avx["family"],
            model=rec_no_avx["size"],
            disk_bytes=disk,
            avx512=False,
        )["blockers"]
        == []
    )
    assert (
        _preflight_for_option(
            tmp_path,
            "avx-yes",
            system="Linux",
            backend="cuda",
            family=rec_avx["family"],
            model=rec_avx["size"],
            disk_bytes=disk,
            avx512=True,
        )["blockers"]
        == []
    )
    q2_entry = next(
        m for m in load_starter_catalog()["polystrata_models"] if m["id"] == "polystrata-qwen38-q2-0"
    )
    with pytest.raises(PreparationError) as exc_q2_avx:
        _preflight_for_option(
            tmp_path,
            "avx-q2-blocked",
            system="Linux",
            backend="cuda",
            family="qwen",
            model="Q2_0",
            disk_bytes=disk,
            avx512=True,
        )
    assert exc_q2_avx.value.reason == "polystrata_disk_insufficient"
    assert (
        exc_q2_avx.value.details["blockers"][0]["required_bytes"]
        == polystrata_total_disk_bytes(q2_entry, system="Linux", avx512=True)
    )


def test_split_volume_runtime_disk_below_minimum_blocks_polystrata(monkeypatch, tmp_path: Path):
    rec_low_runtime = recommend_starter(
        _mac_hw(ram_gib=96, disk_bytes=1 * GIB, models_disk_bytes=200 * GIB)
    )
    assert rec_low_runtime["engine"] == "llama-cpp"
    assert rec_low_runtime["reason"] == "fallback_llama_cpp_insufficient_disk_for_polystrata"

    rec_ok = recommend_starter(
        _mac_hw(ram_gib=96, disk_bytes=2 * GIB, models_disk_bytes=200 * GIB)
    )
    assert rec_ok["engine"] == "polystrata"
    assert rec_ok["size"] == "IQ3_S"

    vol_out = tmp_path / "vol-out"
    vol_data = tmp_path / "vol-data"
    vol_out.mkdir()
    vol_data.mkdir()

    def split_disk_usage(out_free_bytes: int, data_free_bytes: int):
        def _fake_disk_free(path: Path, disk_usage_fn=_disk_usage(data_free_bytes)):
            is_out = path == vol_out or vol_out in path.parents
            free, _ = _real_disk_free(
                path,
                disk_usage_fn=_disk_usage(out_free_bytes if is_out else data_free_bytes),
            )
            return free, (1 if is_out else 2)

        return _fake_disk_free

    monkeypatch.setattr(polystrata_preflight, "_disk_free_bytes", split_disk_usage(1 * GIB, 200 * GIB))
    with pytest.raises(PreparationError) as exc_out:
        run_polystrata_preflight(
            vol_out / "ps",
            vol_data / "ps-data",
            family=rec_ok["family"],
            model=rec_ok["size"],
            backend="metal",
            platform_info=("Darwin", "arm64"),
            macos_version="15.4",
            xcode_clt_check=lambda: True,
        )
    assert exc_out.value.reason == "polystrata_disk_insufficient"

    monkeypatch.setattr(polystrata_preflight, "_disk_free_bytes", split_disk_usage(2 * GIB, 200 * GIB))
    report = run_polystrata_preflight(
        vol_out / "ps-ok",
        vol_data / "ps-data-ok",
        family=rec_ok["family"],
        model=rec_ok["size"],
        backend="metal",
        platform_info=("Darwin", "arm64"),
        macos_version="15.4",
        xcode_clt_check=lambda: True,
    )
    assert report["blockers"] == []


@pytest.mark.parametrize(
    "hw_factory",
    [
        lambda: _mac_hw(ram_gib=96, disk_bytes=200 * GIB),
        lambda: _mac_hw(ram_gib=128, disk_bytes=200 * GIB),
        lambda: _pc_hw(platform_name="Linux", backend="cuda", ram_gib=64, disk_bytes=200 * GIB),
        lambda: _pc_hw(platform_name="Linux", backend="hip", ram_gib=64, disk_bytes=200 * GIB),
    ],
)
def test_high_ram_alternatives_include_lightweight_strata_poly(hw_factory):
    rec = recommend_starter(hw_factory())
    assert rec["engine"] == "polystrata"
    assert rec["engine_core"] == "strata"
    alts = rec["alternatives"]
    alt_ids = [alt["model_id"] for alt in alts]
    assert len(alt_ids) == len(set(alt_ids))
    assert any(alt.get("engine_core") == "strata" for alt in alts)
    poly_alts = [alt for alt in alts if alt.get("engine_core") == "strata-poly"]
    assert poly_alts
    assert poly_alts[0]["model_id"] == "polystrata-qwen36-ud-q4-k-m"
    assert 10_000_000_000 <= poly_alts[0]["estimated_download_bytes"] <= 23_000_000_000
    assert alts[-1]["engine"] == "llama-cpp"


def test_macos_xcode_clt_missing_probe_and_non_blocking_note(monkeypatch, tmp_path: Path):
    existing = tmp_path / "CommandLineTools"
    existing.mkdir()

    monkeypatch.setattr(device_macos, "query", lambda _args: (str(existing), None))
    assert device_macos.xcode_clt_installed() == (True, None)

    monkeypatch.setattr(device_macos, "query", lambda _args: (None, "query_failed"))
    assert device_macos.xcode_clt_installed() == (False, "xcode_clt_missing")

    monkeypatch.setattr(device_macos, "query", lambda _args: (str(tmp_path / "absent"), None))
    assert device_macos.xcode_clt_installed() == (False, "xcode_clt_missing")

    monkeypatch.setattr(device_macos, "query", lambda _args: (None, "probe_timeout"))
    assert device_macos.xcode_clt_installed() == (None, "probe_timeout")

    monkeypatch.setattr(
        device_macos,
        "query",
        lambda args, **_kw: (
            (str(existing), None)
            if args == ["/usr/bin/xcode-select", "-p"]
            else (None, "query_failed")
        ),
    )
    mac_snap = device_macos.snapshot()
    assert mac_snap["xcode_clt_installed"] is True
    assert mac_snap["sources"]["xcode_clt_installed"] == "xcode-select:-p"
    assert str(existing) not in str(mac_snap)

    monkeypatch.setattr(
        device_preflight,
        "platform",
        SimpleNamespace(system=lambda: "Darwin", machine=lambda: "arm64"),
    )
    snap = device_preflight.hardware_snapshot(
        tmp_path,
        environment={"apple_silicon": True, "xcode_clt_installed": False},
    )
    assert snap["xcode_clt_installed"] is False

    mac_missing_clt = _mac_hw(ram_gib=64, disk_bytes=200 * GIB, xcode_clt_installed=False)
    monkeypatch.setattr(device_preflight, "hardware_snapshot", lambda _p: deepcopy(mac_missing_clt))
    report = device_preflight.inspect_device([tmp_path], disk_path=tmp_path)
    starter = report["starter_recommendation"]
    assert starter["status"] == "recommended"
    assert starter["engine"] == "polystrata"
    assert "xcode_clt_missing" in starter["notes"]
    assert report["model_requests_sent"] == 0

    assert (
        "xcode_clt_missing"
        not in recommend_starter(_mac_hw(ram_gib=64, xcode_clt_installed=True))["notes"]
    )
    assert (
        "xcode_clt_missing"
        not in recommend_starter(_mac_hw(ram_gib=16, xcode_clt_installed=False))["notes"]
    )
    assert "xcode_clt_missing" not in recommend_starter(_pc_hw(platform_name="Linux"))["notes"]
    assert (
        "xcode_clt_missing"
        in recommend_starter(
            _mac_hw(ram_gib=64, xcode_clt_installed=None),
            xcode_clt_check=lambda: False,
        )["notes"]
    )
