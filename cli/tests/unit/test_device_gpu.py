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
    unavailable = device_gpu.detect_gpus("Linux")
    assert unavailable["status"] == "unavailable"
    assert unavailable["reason"] == "nvidia_smi_not_found"
    assert unavailable["devices"] == []
    assert unavailable["vendor_reasons"] == {
        "nvidia": "nvidia_smi_not_found",
        "amd": "no_amd_devices",
    }


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


def test_unreadable_drm_and_windows_swap_runtime_error_preserve_report(monkeypatch, tmp_path):
    # Unreadable DRM must not crash detect_gpus or discard NVIDIA observation.
    nv_ok = {
        "status": "observed",
        "source": "nvidia-smi",
        "devices": [{"index": 0, "vendor": "nvidia", "name": "RTX 4070"}],
    }
    monkeypatch.setattr(device_gpu, "nvidia_snapshot", lambda: nv_ok)

    real_is_dir = Path.is_dir

    def denied_is_dir(self):
        if str(self).endswith("class/drm"):
            raise PermissionError("permission denied")
        return real_is_dir(self)

    monkeypatch.setattr(Path, "is_dir", denied_is_dir)
    amd = device_gpu.amd_linux_snapshot(sysfs_root=tmp_path)
    assert amd == {"status": "unavailable", "reason": "drm_sysfs_unreadable", "devices": []}
    assert device_gpu.detect_gpus("Linux") == nv_ok

    # Windows swap_memory raising RuntimeError must not crash _windows().
    class FakeKey:
        def __enter__(self):
            return self

        def __exit__(self, *_args):
            return False

    fake_winreg = types.SimpleNamespace(
        HKEY_LOCAL_MACHINE=1,
        OpenKey=lambda *_args: FakeKey(),
        QueryValueEx=lambda *_args: ("Intel Core i9", 1),
    )

    def broken_swap():
        raise RuntimeError("PdhAddEnglishCounterW failed")

    fake_psutil = types.SimpleNamespace(
        Error=OSError,
        virtual_memory=lambda: types.SimpleNamespace(total=32 * 1024**3, available=20 * 1024**3),
        swap_memory=broken_swap,
        sensors_battery=lambda: types.SimpleNamespace(power_plugged=True),
    )
    monkeypatch.setitem(__import__("sys").modules, "winreg", fake_winreg)
    monkeypatch.setitem(__import__("sys").modules, "psutil", fake_psutil)
    snap = device_host._windows()
    assert snap["memory_total_bytes"] == 32 * 1024**3
    assert snap["page_file_total_bytes"] is None
    assert snap["missing"]["page_file_total_bytes"] == "query_failed"


def test_cli_device_check_survives_drm_permission_error_and_windows_swap_error(
    monkeypatch, capsys
):
    from inferyard.cli import main

    # Linux CLI path: DRM unreadable while NVIDIA succeeds
    nv_ok = {
        "status": "observed",
        "source": "nvidia-smi",
        "devices": [
            {
                "index": 0,
                "vendor": "nvidia",
                "name": "RTX 4070",
                "driver_version": "580.42",
                "compute_capability": "8.9",
                "memory_kind": "dedicated",
                "memory_total_bytes": 12 * 1024**3,
                "memory_free_bytes": 11 * 1024**3,
                "missing": {},
            }
        ],
    }
    monkeypatch.setattr(device_preflight.platform, "system", lambda: "Linux")
    monkeypatch.setattr(device_preflight, "host_snapshot", lambda _s: {"memory_total_bytes": 32 * 1024**3, "mem_available_bytes": 24 * 1024**3, "sources": {}, "missing": {}})
    monkeypatch.setattr(device_gpu, "nvidia_snapshot", lambda: nv_ok)
    real_is_dir = Path.is_dir

    def denied_drm(self):
        if str(self) == "/sys/class/drm":
            raise PermissionError("denied")
        return real_is_dir(self)

    monkeypatch.setattr(Path, "is_dir", denied_drm)
    rc = main(["device-check", "--json"])
    assert rc == 0
    doc = json.loads(capsys.readouterr().out)
    assert doc["details"]["hardware"]["gpu"]["devices"][0]["name"] == "RTX 4070"

    # Windows CLI path: swap_memory RuntimeError preserves report and records reason
    class FakeKey:
        def __enter__(self):
            return self

        def __exit__(self, *_args):
            return False

    monkeypatch.setattr(device_preflight.platform, "system", lambda: "Windows")
    monkeypatch.setattr(device_preflight, "host_snapshot", device_host.snapshot)
    monkeypatch.setattr(
        device_gpu,
        "nvidia_snapshot",
        lambda: {"status": "unavailable", "reason": "nvidia_smi_not_found", "devices": []},
    )
    monkeypatch.setattr(
        device_gpu,
        "amd_windows_snapshot",
        lambda: {"status": "unavailable", "reason": "no_amd_devices", "devices": []},
    )
    monkeypatch.setattr(device_gpu, "_windows_build_number", lambda: 18363)
    fake_winreg = types.SimpleNamespace(
        HKEY_LOCAL_MACHINE=1,
        OpenKey=lambda *_args: FakeKey(),
        QueryValueEx=lambda *_args: ("Intel Core i9", 1),
    )
    fake_psutil = types.SimpleNamespace(
        Error=OSError,
        virtual_memory=lambda: types.SimpleNamespace(total=32 * 1024**3, available=20 * 1024**3),
        swap_memory=lambda: (_ for _ in ()).throw(RuntimeError("pdh failure")),
        sensors_battery=lambda: types.SimpleNamespace(power_plugged=True),
    )
    monkeypatch.setitem(__import__("sys").modules, "winreg", fake_winreg)
    monkeypatch.setitem(__import__("sys").modules, "psutil", fake_psutil)
    rc2 = main(["device-check", "--json"])
    assert rc2 == 0
    doc2 = json.loads(capsys.readouterr().out)
    hw2 = doc2["details"]["hardware"]
    assert hw2["memory_total_bytes"] == 32 * 1024**3
    assert hw2["page_file_total_bytes"] is None
    assert hw2["missing"]["page_file_total_bytes"] == "query_failed"


def test_macos_intel_dual_gpu_and_unknown_vendor():
    payload = json.dumps(
        {
            "SPDisplaysDataType": [
                {
                    "sppci_model": "Intel UHD Graphics 630",
                    "spdisplays_vendor": "sppci_vendor_Intel",
                    "spdisplays_metal": "supported",
                },
                {
                    "sppci_model": "AMD Radeon Pro 5500M",
                    "spdisplays_vendor": "sppci_vendor_AMD",
                    "spdisplays_metal": "supported",
                },
                {
                    "sppci_model": "Generic Display Adapter",
                    "spdisplays_metal": "not supported",
                },
            ]
        }
    )
    snap = device_macos.displays(payload, unified_memory=False)
    assert snap["status"] == "observed"
    d_intel, d_amd, d_unk = snap["devices"]
    assert d_intel["vendor"] == "intel" and "vendor" not in d_intel["missing"]
    assert d_amd["vendor"] == "amd" and "vendor" not in d_amd["missing"]
    assert d_unk["vendor"] is None and d_unk["missing"]["vendor"] == "not_reported"


def test_rocm_smi_correlates_by_pci_bus_not_card_index(tmp_path, monkeypatch):
    drm = tmp_path / "class" / "drm"
    # card0 is Intel
    c0 = drm / "card0" / "device"
    c0.mkdir(parents=True)
    (c0 / "vendor").write_text("0x8086\n", encoding="utf-8")

    # card1 is RX 7900 XTX on bus 0000:03:00.0
    c1 = drm / "card1" / "device"
    c1.mkdir(parents=True)
    (c1 / "vendor").write_text("0x1002\n", encoding="utf-8")
    (c1 / "device").write_text("0x744c\n", encoding="utf-8")
    (c1 / "uevent").write_text("PCI_SLOT_NAME=0000:03:00.0\n", encoding="utf-8")
    (c1 / "mem_info_vram_total").write_text(str(24 * 1024**3), encoding="utf-8")

    # card2 is Radeon 8060S on bus 0000:08:00.0
    c2 = drm / "card2" / "device"
    c2.mkdir(parents=True)
    (c2 / "vendor").write_text("0x1002\n", encoding="utf-8")
    (c2 / "device").write_text("0x1586\n", encoding="utf-8")
    (c2 / "uevent").write_text("PCI_SLOT_NAME=0000:08:00.0\n", encoding="utf-8")

    rocm_json = json.dumps(
        {
            "card0": {"PCI Bus": "0000:03:00.0", "Card Series": "Radeon RX 7900 XTX"},
            "card1": {"PCI Bus": "0000:08:00.0", "Card Series": "Radeon 8060S"},
        }
    )
    monkeypatch.setattr(device_gpu.shutil, "which", lambda name: "/opt/rocm/bin/rocm-smi")
    monkeypatch.setattr(
        device_gpu.subprocess,
        "run",
        lambda cmd, **_kw: subprocess.CompletedProcess(cmd, 0, stdout=rocm_json, stderr=""),
    )
    bus_map = device_gpu._rocm_smi_names_by_bus()
    assert bus_map == {
        "0000:03:00.0": "Radeon RX 7900 XTX",
        "0000:08:00.0": "Radeon 8060S",
    }

    snap = device_gpu.amd_linux_snapshot(sysfs_root=tmp_path, rocm_names=bus_map)
    assert snap["devices"][0]["index"] == 1
    assert snap["devices"][0]["name"] == "Radeon RX 7900 XTX"
    assert snap["devices"][0]["pci_device_id"] == "0x744c"
    assert snap["devices"][0]["arch_family"] == "gfx1100"
    assert snap["devices"][1]["index"] == 2
    assert snap["devices"][1]["name"] == "Radeon 8060S"
    assert snap["devices"][1]["pci_device_id"] == "0x1586"
    assert snap["devices"][1]["arch_family"] == "gfx1151"


def test_missing_reasons_for_null_amd_and_non_windows_page_file(tmp_path, monkeypatch):
    # Linux AMD missing PCI ID
    drm = tmp_path / "class" / "drm"
    dev = drm / "card0" / "device"
    dev.mkdir(parents=True)
    (dev / "vendor").write_text("0x1002\n", encoding="utf-8")
    linux_amd = device_gpu.amd_linux_snapshot(sysfs_root=tmp_path)["devices"][0]
    assert linux_amd["pci_device_id"] is None
    assert linux_amd["missing"]["pci_device_id"] == "not_reported"

    # Windows AMD missing driver and PCI ID
    win_amd = device_gpu._parse_windows_amd_adapters(json.dumps({"Name": "AMD Radeon Graphics"}))[0]
    assert win_amd["driver_version"] is None
    assert win_amd["pci_device_id"] is None
    assert win_amd["missing"]["driver_version"] == "not_reported"
    assert win_amd["missing"]["pci_device_id"] == "not_reported"

    # Both NVIDIA missing and Windows AMD timeout preserve vendor_reasons + missing["gpu.amd"]
    monkeypatch.setattr(device_preflight.platform, "system", lambda: "Windows")
    monkeypatch.setattr(
        device_gpu,
        "nvidia_snapshot",
        lambda: {"status": "unavailable", "reason": "nvidia_smi_not_found", "devices": []},
    )
    monkeypatch.setattr(
        device_gpu,
        "amd_windows_snapshot",
        lambda: {"status": "unavailable", "reason": "probe_timeout", "devices": []},
    )
    monkeypatch.setattr(
        device_preflight,
        "cpu_features_snapshot",
        lambda _sys: ({"avx2": None, "avx512": None}, "test", {"avx2": "unsupported_windows_build"}),
    )
    hw_win = device_preflight.hardware_snapshot(tmp_path, environment={})
    assert hw_win["gpu"]["reason"] == "nvidia_smi_not_found"
    assert hw_win["gpu"]["vendor_reasons"] == {
        "nvidia": "nvidia_smi_not_found",
        "amd": "probe_timeout",
    }
    assert hw_win["missing"]["gpu"] == "nvidia_smi_not_found"
    assert hw_win["missing"]["gpu.amd"] == "probe_timeout"

    # Non-Windows marks page_file_total_bytes as not_applicable_non_windows
    monkeypatch.setattr(device_preflight.platform, "system", lambda: "Darwin")
    hw_mac = device_preflight.hardware_snapshot(tmp_path, environment={})
    assert hw_mac["page_file_total_bytes"] is None
    assert hw_mac["missing"]["page_file_total_bytes"] == "not_applicable_non_windows"


def test_models_disk_follows_explicit_model_selected_root_and_multi_volume(tmp_path, monkeypatch):
    from tests.unit.test_device_preflight import gguf

    root_a = tmp_path / "vol_a"
    root_b = tmp_path / "vol_b"
    root_a.mkdir()
    root_b.mkdir()
    model_b = gguf(root_b / "demo.gguf")

    fake_usage = {
        str(root_a.resolve()): 101 * device_preflight.GIB,
        str(root_b.resolve()): 7 * device_preflight.GIB,
    }
    def fake_disk_usage(p):
        free = fake_usage.get(str(Path(p).resolve()), 50 * device_preflight.GIB)
        return types.SimpleNamespace(free=free)

    monkeypatch.setattr(device_preflight.shutil, "disk_usage", fake_disk_usage)
    monkeypatch.setattr(
        device_preflight,
        "hardware_snapshot",
        lambda _p: {
            "platform": "Linux",
            "memory_available_bytes": 16 * device_preflight.GIB,
            "logical_cpus": 8,
            "disk_free_bytes": 50 * device_preflight.GIB,
            "gpu": {"status": "unavailable", "reason": "nvidia_smi_not_found", "devices": []},
            "sources": {},
            "missing": {},
        },
    )

    # 1) Explicit --model measures model_b's parent volume, not default artifacts
    rep_explicit = device_preflight.inspect_device([root_a], model_path=model_b, disk_path=tmp_path)
    assert rep_explicit["hardware"]["models_disk_path"] == str(root_b.resolve())
    assert rep_explicit["hardware"]["models_disk_free_bytes"] == 7 * device_preflight.GIB
    # 2) Multiple --models-root with model in root_b picks model_b's volume
    rep_multi_sel = device_preflight.inspect_device([root_a, root_b], disk_path=tmp_path)
    assert rep_multi_sel["hardware"]["models_disk_path"] == str(root_b.resolve())
    assert rep_multi_sel["hardware"]["models_disk_free_bytes"] == 7 * device_preflight.GIB

    # 3) Multiple --models-root on distinct devices with no models leaves models_disk_free_bytes null
    empty_a = tmp_path / "empty_a"
    empty_b = tmp_path / "empty_b"
    empty_a.mkdir()
    empty_b.mkdir()
    real_stat = Path.stat

    def distinct_stat(self, *args, **kwargs):
        st = real_stat(self, *args, **kwargs)
        if self.resolve() == empty_a.resolve():
            return types.SimpleNamespace(st_dev=101)
        if self.resolve() == empty_b.resolve():
            return types.SimpleNamespace(st_dev=202)
        return st

    monkeypatch.setattr(Path, "stat", distinct_stat)
    rep_unselected = device_preflight.inspect_device([empty_a, empty_b], disk_path=tmp_path)
    assert rep_unselected["hardware"]["models_disk_free_bytes"] is None
    assert rep_unselected["hardware"]["missing"]["models_disk_free_bytes"] == (
        "multiple_models_roots_unselected"
    )
    assert len(rep_unselected["hardware"]["models_disk_roots"]) == 2


def test_windows_cpu_features_gates_on_build_19041(monkeypatch):
    # Pre-2004 build returns None + unsupported_windows_build
    monkeypatch.setattr(device_gpu, "_windows_build_number", lambda: 18363)
    feats, _src, missing = device_gpu._windows_cpu_features()
    assert feats == {"avx2": None, "avx512": None}
    assert missing == {
        "avx2": "unsupported_windows_build",
        "avx512": "unsupported_windows_build",
    }

    # Supported build >= 19041 queries IsProcessorFeaturePresent
    monkeypatch.setattr(device_gpu, "_windows_build_number", lambda: 19045)
    fake_kernel32 = types.SimpleNamespace(IsProcessorFeaturePresent=lambda code: 1 if code == 40 else 0)
    fake_ctypes = types.SimpleNamespace(windll=types.SimpleNamespace(kernel32=fake_kernel32))
    monkeypatch.setitem(__import__("sys").modules, "ctypes", fake_ctypes)
    feats, _src, missing = device_gpu._windows_cpu_features()
    assert feats == {"avx2": True, "avx512": False}
    assert missing == {}

