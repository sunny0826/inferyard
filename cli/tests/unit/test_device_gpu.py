"""Cross-platform GPU, CPU feature, page file, OS version, and models-disk probes."""

from __future__ import annotations

import json
import subprocess
import types
from pathlib import Path

from inferyard.platforms import device_gpu, device_host, device_macos, device_preflight


def test_nvidia_snapshot_reads_compute_cap(monkeypatch):
    def fake_run(cmd, **_kwargs):
        assert "compute_cap" in cmd[1]
        return subprocess.CompletedProcess(
            cmd,
            0,
            stdout="0, NVIDIA GeForce RTX 4070, 580.42, 12288, 11000, 12, 41, 18.5, 8.9\n",
            stderr="",
        )

    monkeypatch.setattr(device_gpu.shutil, "which", lambda name: "/usr/bin/nvidia-smi")
    monkeypatch.setattr(device_gpu.subprocess, "run", fake_run)

    snap = device_gpu.nvidia_snapshot()
    assert snap["status"] == "observed"
    assert snap["source"] == "nvidia-smi"
    dev = snap["devices"][0]
    assert dev["vendor"] == "nvidia"
    assert dev["compute_capability"] == "8.9"
    assert dev["memory_kind"] == "dedicated"
    assert dev["memory_total_bytes"] == 12288 * 1024**2
    assert dev["memory_free_bytes"] == 11000 * 1024**2
    assert dev["missing"] == {}


def test_nvidia_snapshot_falls_back_when_compute_cap_unsupported(monkeypatch):
    calls = []

    def fake_run(cmd, **_kwargs):
        calls.append(cmd[1])
        if "compute_cap" in cmd[1]:
            return subprocess.CompletedProcess(cmd, 2, stdout="", stderr="invalid field")
        return subprocess.CompletedProcess(
            cmd,
            0,
            stdout="0, NVIDIA GeForce RTX 3090, 535.10, 24576, 20000, N/A, 50, Not Supported\n",
            stderr="",
        )

    monkeypatch.setattr(device_gpu.shutil, "which", lambda name: "/usr/bin/nvidia-smi")
    monkeypatch.setattr(device_gpu.subprocess, "run", fake_run)

    snap = device_gpu.nvidia_snapshot()
    assert len(calls) == 2
    assert snap["status"] == "observed"
    dev = snap["devices"][0]
    assert dev["vendor"] == "nvidia"
    assert dev["compute_capability"] is None
    assert dev["missing"]["compute_capability"] == "not_reported"
    assert dev["missing"]["utilization_percent"] == "not_reported"
    assert dev["missing"]["power_w"] == "not_reported"


def test_nvidia_snapshot_timeout_and_missing(monkeypatch):
    monkeypatch.setattr(device_gpu.shutil, "which", lambda name: None)
    assert device_gpu.nvidia_snapshot() == {
        "status": "unavailable",
        "reason": "nvidia_smi_not_found",
        "devices": [],
    }

    monkeypatch.setattr(device_gpu.shutil, "which", lambda name: "/usr/bin/nvidia-smi")

    def boom(cmd, **_kwargs):
        raise subprocess.TimeoutExpired(cmd, 8)

    monkeypatch.setattr(device_gpu.subprocess, "run", boom)
    assert device_gpu.nvidia_snapshot() == {
        "status": "unavailable",
        "reason": "probe_timeout",
        "devices": [],
    }


def test_amd_linux_snapshot_parses_sysfs_and_keeps_missing_null(tmp_path):
    drm = tmp_path / "class" / "drm"
    # Non-AMD card should be ignored
    intel = drm / "card0" / "device"
    intel.mkdir(parents=True)
    (intel / "vendor").write_text("0x8086\n", encoding="utf-8")

    # AMD RDNA3 card with full VRAM stats
    amd1 = drm / "card1" / "device"
    amd1.mkdir(parents=True)
    (amd1 / "vendor").write_text("0x1002\n", encoding="utf-8")
    (amd1 / "device").write_text("0x744c\n", encoding="utf-8")
    (amd1 / "mem_info_vram_total").write_text(str(24 * 1024**3) + "\n", encoding="utf-8")
    (amd1 / "mem_info_vram_used").write_text(str(2 * 1024**3) + "\n", encoding="utf-8")

    # AMD card with unknown PCI ID and missing VRAM files
    amd2 = drm / "card2" / "device"
    amd2.mkdir(parents=True)
    (amd2 / "vendor").write_text("0x1002\n", encoding="utf-8")
    (amd2 / "device").write_text("0x9999\n", encoding="utf-8")

    # Connector symlink-like directory should be ignored
    (drm / "card1-DP-1").mkdir(parents=True)

    snap = device_gpu.amd_linux_snapshot(sysfs_root=tmp_path)
    assert snap["status"] == "observed"
    assert snap["source"] == "sysfs_drm"
    assert len(snap["devices"]) == 2

    d1, d2 = snap["devices"]
    assert d1["vendor"] == "amd"
    assert d1["arch_family"] == "gfx1100"
    assert d1["memory_total_bytes"] == 24 * 1024**3
    assert d1["memory_free_bytes"] == 22 * 1024**3
    assert d1["missing"] == {}

    assert d2["vendor"] == "amd"
    assert d2["arch_family"] is None
    assert d2["memory_total_bytes"] is None
    assert d2["memory_free_bytes"] is None
    assert d2["missing"]["memory_total_bytes"] == "not_reported"
    assert d2["missing"]["arch_family"] == "not_reported"


def test_amd_windows_snapshot_does_not_fabricate_vram():
    payload = json.dumps(
        [
            {
                "Name": "Intel(R) UHD Graphics",
                "DriverVersion": "31.0.1",
                "AdapterRAM": 1073741824,
                "PNPDeviceID": "PCI\\VEN_8086&DEV_4680",
            },
            {
                "Name": "AMD Radeon RX 9070 XT",
                "DriverVersion": "32.0.13031.3015",
                "AdapterRAM": 4293918720,
                "PNPDeviceID": "PCI\\VEN_1002&DEV_7550&SUBSYS_0000",
            },
        ]
    )
    devices = device_gpu._parse_windows_amd_adapters(payload)
    assert len(devices) == 1
    dev = devices[0]
    assert dev["vendor"] == "amd"
    assert dev["name"] == "AMD Radeon RX 9070 XT"
    assert dev["arch_family"] == "gfx1201"
    assert dev["memory_total_bytes"] is None
    assert dev["memory_free_bytes"] is None
    assert dev["missing"]["memory_total_bytes"] == "windows_wmi_32bit_cap"


def test_detect_gpus_merges_nvidia_and_amd_and_preserves_fallback_reason(monkeypatch):
    nv_ok = {
        "status": "observed",
        "source": "nvidia-smi",
        "devices": [{"index": 0, "vendor": "nvidia", "name": "RTX 4090"}],
    }
    amd_ok = {
        "status": "observed",
        "source": "sysfs_drm",
        "devices": [{"index": 1, "vendor": "amd", "name": "RX 7900 XTX"}],
    }
    monkeypatch.setattr(device_gpu, "nvidia_snapshot", lambda: nv_ok)
    monkeypatch.setattr(device_gpu, "amd_linux_snapshot", lambda: amd_ok)
    merged = device_gpu.detect_gpus("Linux")
    assert merged["status"] == "observed"
    assert merged["source"] == "nvidia-smi+sysfs_drm"
    assert [d["vendor"] for d in merged["devices"]] == ["nvidia", "amd"]

    nv_miss = {"status": "unavailable", "reason": "nvidia_smi_not_found", "devices": []}
    amd_miss = {"status": "unavailable", "reason": "no_amd_devices", "devices": []}
    monkeypatch.setattr(device_gpu, "nvidia_snapshot", lambda: nv_miss)
    monkeypatch.setattr(device_gpu, "amd_linux_snapshot", lambda: amd_miss)
    assert device_gpu.detect_gpus("Linux") == nv_miss


def test_cpu_features_linux_and_macos(tmp_path, monkeypatch):
    cpuinfo = tmp_path / "cpuinfo"
    cpuinfo.write_text("processor : 0\nflags : fpu sse4_2 avx avx2\n", encoding="utf-8")
    feats, src, missing = device_gpu._linux_cpu_features(cpuinfo)
    assert feats == {"avx2": True, "avx512": False}
    assert src == "/proc/cpuinfo:flags"
    assert missing == {}

    cpuinfo.write_text("processor : 0\nHardware : Generic ARM\n", encoding="utf-8")
    feats, _src, missing = device_gpu._linux_cpu_features(cpuinfo)
    assert feats == {"avx2": None, "avx512": None}
    assert missing["avx2"] == "not_reported"

    monkeypatch.setattr(device_gpu.platform, "machine", lambda: "arm64")
    feats, _src, missing = device_gpu._macos_cpu_features()
    assert feats == {"avx2": None, "avx512": None}
    assert missing["avx2"] == "not_applicable_arm64"


def test_macos_snapshot_reports_os_version_and_apple_vendor(monkeypatch):
    def fake_query(args, **_kwargs):
        if args == ["/usr/bin/sw_vers", "-productVersion"]:
            return "15.4", None
        if args[:2] == ["/usr/sbin/sysctl", "-n"]:
            mapping = {
                "machdep.cpu.brand_string": "Apple M4 Max",
                "hw.logicalcpu": "16",
                "hw.physicalcpu": "16",
                "hw.memsize": str(64 * 1024**3),
                "hw.optional.arm64": "1",
            }
            return mapping[args[2]], None
        if args == ["/usr/bin/vm_stat"]:
            return (
                "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n"
                "Pages free: 1000.\nPages inactive: 2000.\nPages speculative: 500.\n",
                None,
            )
        if args == ["/usr/bin/pmset", "-g", "batt"]:
            return "Now drawing from 'AC Power'", None
        if args[0] == "/usr/sbin/system_profiler":
            return (
                json.dumps(
                    {
                        "SPDisplaysDataType": [
                            {
                                "sppci_model": "Apple M4 Max",
                                "sppci_cores": "40",
                                "spdisplays_mtlgpufamilysupport": "spdisplays_metal3",
                            }
                        ]
                    }
                ),
                None,
            )
        return None, "query_failed"

    monkeypatch.setattr(device_macos, "query", fake_query)
    snap = device_macos.snapshot()
    assert snap["os_version"] == "15.4"
    assert snap["sources"]["os_version"] == "sw_vers:productVersion"
    assert snap["apple_gpu"]["devices"][0]["vendor"] == "apple"


def test_windows_host_snapshot_reads_page_file(monkeypatch):
    class FakeKey:
        def __enter__(self):
            return self

        def __exit__(self, *_args):
            return False

    fake_winreg = types.SimpleNamespace(
        HKEY_LOCAL_MACHINE=1,
        OpenKey=lambda *_args: FakeKey(),
        QueryValueEx=lambda *_args: ("AMD Ryzen 9 7950X", 1),
    )
    fake_psutil = types.SimpleNamespace(
        Error=RuntimeError,
        virtual_memory=lambda: types.SimpleNamespace(total=64 * 1024**3, available=40 * 1024**3),
        swap_memory=lambda: types.SimpleNamespace(total=32 * 1024**3),
        sensors_battery=lambda: types.SimpleNamespace(power_plugged=True),
    )
    monkeypatch.setitem(__import__("sys").modules, "winreg", fake_winreg)
    monkeypatch.setitem(__import__("sys").modules, "psutil", fake_psutil)

    snap = device_host._windows()
    assert snap["page_file_total_bytes"] == 32 * 1024**3
    assert snap["sources"]["page_file_total_bytes"] == "psutil.swap_memory"
    assert "page_file_total_bytes" not in snap["missing"]


def test_inspect_device_attaches_models_disk_and_recommend_ignores_amd_for_cuda(tmp_path):
    report = device_preflight.inspect_device([tmp_path], disk_path=tmp_path)
    hw = report["hardware"]
    assert hw["models_disk_path"] == str(Path(tmp_path).resolve())
    assert isinstance(hw["models_disk_free_bytes"], int) and hw["models_disk_free_bytes"] > 0

    amd_hw = {
        "platform": "Linux",
        "memory_available_bytes": 32 * device_preflight.GIB,
        "logical_cpus": 16,
        "disk_free_bytes": 100 * device_preflight.GIB,
        "gpu": {
            "status": "observed",
            "devices": [
                {
                    "index": 0,
                    "vendor": "amd",
                    "name": "AMD Radeon RX 7900 XTX",
                    "memory_total_bytes": 24 * device_preflight.GIB,
                    "memory_free_bytes": 22 * device_preflight.GIB,
                }
            ],
        },
    }
    rec = device_preflight.recommend(
        amd_hw,
        [
            {
                "path": "m.gguf",
                "name": "M",
                "size_bytes": 2 * device_preflight.GIB,
                "context_length": 4096,
                "status": "available",
            }
        ],
    )
    assert rec["selected"]["mode"] == "cpu"
