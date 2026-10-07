"""Native device probes preserve partial observations, units and privacy across platforms."""

import json
import subprocess
import sys
from types import SimpleNamespace

import pytest

from inferyard.platforms import device_host, device_macos, device_preflight
from inferyard.platforms.device_preflight import GIB


@pytest.mark.parametrize("page_size", [4096, 16384])
def test_mac_available_estimate_does_not_count_purgeable_or_compressed_twice(page_size):
    text = f"""Mach Virtual Memory Statistics: (page size of {page_size} bytes)
Pages free: 10.
Pages inactive: 20.
Pages speculative: 30.
Pages purgeable: 999.
Pages occupied by compressor: 999.
"""
    assert device_macos.available_memory(text) == 60 * page_size
    assert device_macos.available_memory(text.replace("Pages free:", "Missing:")) is None


@pytest.mark.parametrize("metal_key", ["spdisplays_metal", "spdisplays_mtlgpufamilysupport"])
@pytest.mark.parametrize("silicon", [True, False])
def test_mac_display_whitelist_preserves_shared_memory_and_excludes_serials(metal_key, silicon):
    raw = json.dumps(
        {
            "SPDisplaysDataType": [
                {
                    "sppci_model": "Apple GPU" if silicon else "Intel Iris",
                    "sppci_cores": "10",
                    metal_key: "spdisplays_metal4",
                    "serial_number": "SENSITIVE",
                    "spdisplays_ndrvs": [{"serial": "SENSITIVE"}],
                }
            ]
        }
    )
    report = device_macos.displays(raw, unified_memory=silicon)
    gpu = report["devices"][0]
    assert gpu["metal_supported"] is True and gpu["cores"] == 10
    assert gpu["memory_kind"] == ("shared_host" if silicon else "unmeasured")
    assert gpu["memory_total_bytes"] is None and gpu["memory_free_bytes"] is None
    assert "SENSITIVE" not in json.dumps(report)


@pytest.mark.parametrize(
    "raw",
    [
        "null",
        "{}",
        '{"SPDisplaysDataType": {}}',
        "invalid",
        '{"SPDisplaysDataType": [], "SPDisplaysDataType": []}',
        '{"SPDisplaysDataType": [{"sppci_model": "GPU", "sppci_cores": 1e309}]}',
    ],
)
def test_mac_invalid_profiler_is_missing_instead_of_crashing(raw):
    report = device_macos.displays(raw, unified_memory=True)
    assert report == {"status": "unavailable", "reason": "invalid_output", "devices": []}


@pytest.mark.parametrize("cores,metal", [(True, ""), (1.5, "unknown"), (0, None)])
def test_mac_unknown_metal_and_invalid_core_counts_stay_missing(cores, metal):
    raw = json.dumps(
        {
            "SPDisplaysDataType": [
                {"sppci_model": "GPU", "sppci_cores": cores, "spdisplays_metal": metal}
            ]
        }
    )
    gpu = device_macos.displays(raw, unified_memory=True)["devices"][0]
    assert gpu["cores"] is None and gpu["metal_supported"] is None


@pytest.mark.parametrize(
    "exception,reason",
    [
        (FileNotFoundError(), "command_not_found"),
        (PermissionError(), "permission_denied"),
        (subprocess.TimeoutExpired("query", 2), "probe_timeout"),
    ],
)
def test_mac_system_query_has_bounded_failures(monkeypatch, exception, reason):
    def run(arguments, **kwargs):
        assert kwargs["timeout"] == 2
        raise exception

    monkeypatch.setattr(device_macos.subprocess, "run", run)
    assert device_macos.query(["query"]) == (None, reason)


def test_mac_snapshot_retains_partial_fields_and_query_sources(monkeypatch):
    def query(arguments, **kwargs):
        if arguments[0].endswith("sysctl"):
            return {
                "machdep.cpu.brand_string": "Apple M4",
                "hw.memsize": str(16 * GIB),
                "hw.logicalcpu": "10",
                "hw.physicalcpu": "10",
                "hw.optional.arm64": "1",
            }[arguments[-1]], None
        if arguments[0].endswith("vm_stat"):
            return None, "probe_timeout"
        if arguments[0].endswith("pmset"):
            return "Now drawing from 'AC Power'", None
        assert kwargs["timeout"] == 6
        return None, "permission_denied"

    monkeypatch.setattr(device_macos, "query", query)
    report = device_macos.snapshot()
    assert report["cpu_model"] == "Apple M4" and report["memory_total_bytes"] == 16 * GIB
    assert report["ac_online"] is True and report["apple_silicon"] is True
    assert report["mem_available_bytes"] is None
    assert report["missing"]["memory_available_bytes"] == "probe_timeout"
    assert report["sources"]["memory_available_bytes"] == "vm_stat:free+inactive+speculative"
    assert report["apple_gpu"]["reason"] == "permission_denied"


def test_linux_arm_cpu_and_memory_are_read_independently(monkeypatch):
    files = {
        "/proc/cpuinfo": "processor: 0\nHardware: ARM device",
        "/proc/meminfo": "MemTotal: 8388608 kB\nMemAvailable: broken kB",
    }
    monkeypatch.setattr(device_host, "_read", lambda path: files.get(str(path)))
    report = device_host.snapshot("Linux")
    assert report["cpu_model"] == "ARM device" and report["memory_total_bytes"] == 8 * GIB
    assert report["mem_available_bytes"] is None


@pytest.mark.parametrize("memory_fails", [True, False])
def test_windows_partial_memory_probe_does_not_erase_cpu_and_power(monkeypatch, memory_fails):
    class Key:
        def __enter__(self):
            return self

        def __exit__(self, *args):
            pass

    def memory():
        if memory_fails:
            raise OSError("private details")
        return SimpleNamespace(total=16 * GIB, available=8 * GIB)

    monkeypatch.setitem(
        sys.modules,
        "winreg",
        SimpleNamespace(
            HKEY_LOCAL_MACHINE=1,
            OpenKey=lambda *args: Key(),
            QueryValueEx=lambda *args: ("Test CPU", 1),
        ),
    )
    monkeypatch.setitem(
        sys.modules,
        "psutil",
        SimpleNamespace(
            Error=RuntimeError,
            virtual_memory=memory,
            sensors_battery=lambda: SimpleNamespace(power_plugged=True),
        ),
    )
    report = device_host.snapshot("Windows")
    assert report["cpu_model"] == "Test CPU" and report["ac_online"] is True
    assert report.get("mem_available_bytes") == (None if memory_fails else 8 * GIB)
    if memory_fails:
        assert report["missing"]["memory_available_bytes"] == "query_failed"


@pytest.mark.parametrize("system", ["Darwin", "Linux", "Windows"])
def test_hardware_snapshot_dispatches_and_preserves_missing_sources(monkeypatch, tmp_path, system):
    observed = []

    def snapshot(value):
        observed.append(value)
        return {
            "cpu_model": "Known CPU",
            "mem_available_bytes": None,
            "sources": {"memory_available_bytes": "native_source"},
            "missing": {"memory_available_bytes": "probe_timeout"},
        }

    monkeypatch.setattr(device_preflight.platform, "system", lambda: system)
    monkeypatch.setattr(device_preflight, "host_snapshot", snapshot)
    monkeypatch.setattr(
        device_preflight,
        "nvidia_snapshot",
        lambda: {"status": "unavailable", "reason": "nvidia_smi_not_found", "devices": []},
    )
    report = device_preflight.hardware_snapshot(tmp_path)
    assert observed == [system] and report["cpu_model"] == "Known CPU"
    assert report["memory_available_bytes"] is None
    assert report["missing"]["memory_available_bytes"] == "probe_timeout"
    assert report["sources"]["memory_available_bytes"] == "native_source"
    assert report["disk_path"] == str(tmp_path.resolve())
    capabilities = device_preflight.platform_capabilities(system)
    assert capabilities["live_single_run"]
    assert capabilities["live_experiments"] == (system != "Windows")
    assert not capabilities["metal_backend_verified"]


def test_runtime_environment_injection_does_not_reprobe_host(monkeypatch, tmp_path):
    monkeypatch.setattr(device_preflight.platform, "system", lambda: "Linux")

    def forbidden(system):
        pytest.fail("runtime snapshot must retain its original host observation")

    monkeypatch.setattr(device_preflight, "host_snapshot", forbidden)
    monkeypatch.setattr(
        device_preflight, "nvidia_snapshot", lambda: {"status": "unavailable", "devices": []}
    )
    report = device_preflight.hardware_snapshot(
        tmp_path,
        {
            "mem_available_bytes": 7 * GIB,
            "memory_total_bytes": 16 * GIB,
            "cpu_model": "Bound CPU",
            "ac_online": False,
        },
    )
    assert report["memory_available_bytes"] == 7 * GIB and report["ac_online"] is False
    assert report["sources"]["cpu_model"] == "runtime_environment"


@pytest.mark.parametrize("optional", ["NaN", "Infinity", "N/A", "[Not Supported]"])
def test_nvidia_nonfinite_or_missing_sensor_values_remain_json_safe(monkeypatch, optional):
    monkeypatch.setattr(device_preflight.shutil, "which", lambda _: "nvidia-smi")
    monkeypatch.setattr(
        device_preflight.subprocess,
        "run",
        lambda *args, **kwargs: SimpleNamespace(
            returncode=0, stdout=f"0, GPU, 1.0, 8192, 4096, {optional}, {optional}, {optional}\n"
        ),
    )
    report = device_preflight.nvidia_snapshot()
    device = report["devices"][0]
    assert report["status"] == "observed" and device["memory_total_bytes"] == 8 * GIB
    assert all(
        device[field] is None for field in ("utilization_percent", "temperature_c", "power_w")
    )
    assert len(device["missing"]) == 3
    json.dumps(report, allow_nan=False)


@pytest.mark.parametrize("memory", ["NaN", "Infinity", "-1", "N/A"])
def test_nvidia_invalid_memory_cannot_be_capacity_recommendation(monkeypatch, memory):
    monkeypatch.setattr(device_preflight.shutil, "which", lambda _: "nvidia-smi")
    monkeypatch.setattr(
        device_preflight.subprocess,
        "run",
        lambda *args, **kwargs: SimpleNamespace(
            returncode=0, stdout=f"0, GPU, 1.0, 8192, {memory}, 0, 40, 10\n"
        ),
    )
    assert device_preflight.nvidia_snapshot()["status"] == "unavailable"
