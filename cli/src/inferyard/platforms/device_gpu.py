"""Cross-platform read-only GPU (NVIDIA/AMD) and CPU instruction feature probes."""

from __future__ import annotations

import csv
import io
import json
import math
import os
import platform
import re
import shutil
import subprocess
from pathlib import Path

_NVIDIA_BASE_QUERY = (
    "index,name,driver_version,memory.total,memory.free,utilization.gpu,temperature.gpu,power.draw"
)
_NVIDIA_EXTENDED_QUERY = _NVIDIA_BASE_QUERY + ",compute_cap"
_COMPUTE_CAP_RE = re.compile(r"^\d+\.\d+$")

# Common discrete AMD PCI device IDs mapped to ROCm/HIP gfx architecture families.
_AMD_PCI_ARCH = {
    "0x73bf": "gfx1030",  # RX 6800 / 6800 XT / 6900 XT / 6950 XT
    "0x73df": "gfx1031",  # RX 6700 XT / 6750 XT
    "0x744c": "gfx1100",  # RX 7900 XT / 7900 XTX / PRO W7900
    "0x7448": "gfx1100",  # Radeon PRO W7900
    "0x747e": "gfx1101",  # RX 7700 XT / 7800 XT
    "0x7480": "gfx1102",  # RX 7600 / 7600 XT
    "0x7550": "gfx1201",  # RX 9070 / 9070 XT / AI PRO R9700
    "0x7590": "gfx1200",  # RX 9060 XT
    "0x1586": "gfx1151",  # Strix Halo (Radeon 8060S / 8050S)
}


def _no_window_flags() -> int:
    return getattr(subprocess, "CREATE_NO_WINDOW", 0) if os.name == "nt" else 0


def _read_text(path: Path) -> str | None:
    try:
        return path.read_text(encoding="utf-8", errors="replace").strip()
    except OSError:
        return None


def _parse_nvidia_rows(stdout: str, *, expect_compute_cap: bool) -> list[dict]:
    expected_cols = 9 if expect_compute_cap else 8
    devices: list[dict] = []
    for row in csv.reader(io.StringIO(stdout)):
        if len(row) != expected_cols:
            raise ValueError("invalid_nvidia_query")
        values = [cell.strip() for cell in row]
        missing: dict[str, str] = {}

        def number(cell: str, field: str, *, required: bool = False, missing: dict = missing):
            try:
                value = float(cell)
                if not math.isfinite(value) or (value < 0 and field != "temperature_c"):
                    raise ValueError
                if field == "utilization_percent" and value > 100:
                    raise ValueError
                return value
            except ValueError:
                if required:
                    raise ValueError("invalid_nvidia_query") from None
                missing[field] = (
                    "not_reported"
                    if "N/A" in cell or "Not Supported" in cell
                    else "invalid_output"
                )
                return None

        total = int(number(values[3], "memory_total_bytes", required=True) * 1024**2)
        free = int(number(values[4], "memory_free_bytes", required=True) * 1024**2)
        index = int(values[0])
        if index < 0 or not values[1] or total <= 0 or free > total:
            raise ValueError("invalid_nvidia_query")

        compute_cap: str | None = None
        if expect_compute_cap:
            raw_cap = values[8]
            if _COMPUTE_CAP_RE.fullmatch(raw_cap):
                compute_cap = raw_cap
            else:
                missing["compute_capability"] = (
                    "not_reported"
                    if "N/A" in raw_cap or "Not Supported" in raw_cap or not raw_cap
                    else "invalid_output"
                )
        else:
            missing["compute_capability"] = "not_reported"

        devices.append(
            {
                "index": index,
                "vendor": "nvidia",
                "name": values[1],
                "driver_version": values[2],
                "compute_capability": compute_cap,
                "memory_kind": "dedicated",
                "memory_total_bytes": total,
                "memory_free_bytes": free,
                "utilization_percent": number(values[5], "utilization_percent"),
                "temperature_c": number(values[6], "temperature_c"),
                "power_w": number(values[7], "power_w"),
                "missing": missing,
            }
        )
    if not devices:
        raise ValueError("no_nvidia_devices")
    return devices


def nvidia_snapshot() -> dict:
    executable = shutil.which("nvidia-smi")
    if executable is None:
        return {"status": "unavailable", "reason": "nvidia_smi_not_found", "devices": []}
    try:
        result = subprocess.run(
            [executable, "--query-gpu=" + _NVIDIA_EXTENDED_QUERY, "--format=csv,noheader,nounits"],
            capture_output=True,
            text=True,
            timeout=8,
            creationflags=_no_window_flags(),
        )
        if result.returncode == 0:
            devices = _parse_nvidia_rows(result.stdout, expect_compute_cap=True)
            return {"status": "observed", "source": "nvidia-smi", "devices": devices}
        # Fallback for older nvidia-smi builds that do not support compute_cap.
        fallback = subprocess.run(
            [executable, "--query-gpu=" + _NVIDIA_BASE_QUERY, "--format=csv,noheader,nounits"],
            capture_output=True,
            text=True,
            timeout=8,
            creationflags=_no_window_flags(),
        )
        if fallback.returncode:
            raise ValueError("nvidia_driver_query_failed")
        devices = _parse_nvidia_rows(fallback.stdout, expect_compute_cap=False)
        return {"status": "observed", "source": "nvidia-smi", "devices": devices}
    except subprocess.TimeoutExpired:
        return {"status": "unavailable", "reason": "probe_timeout", "devices": []}
    except OSError, UnicodeError, ValueError, OverflowError:
        return {"status": "unavailable", "reason": "nvidia_driver_query_failed", "devices": []}


def _positive_int(raw: str | None) -> int | None:
    if raw is None:
        return None
    try:
        value = int(raw.strip())
    except ValueError:
        return None
    return value if value > 0 else None


def _nonnegative_int(raw: str | None) -> int | None:
    if raw is None:
        return None
    try:
        value = int(raw.strip())
    except ValueError:
        return None
    return value if value >= 0 else None


def _rocm_smi_names() -> dict[int, str]:
    executable = shutil.which("rocm-smi")
    if executable is None:
        return {}
    try:
        result = subprocess.run(
            [executable, "--showproductname", "--json"],
            capture_output=True,
            text=True,
            timeout=5,
        )
        if result.returncode:
            return {}
        data = json.loads(result.stdout)
        if not isinstance(data, dict):
            return {}
        names: dict[int, str] = {}
        for key, value in data.items():
            if not isinstance(value, dict):
                continue
            match = re.search(r"(\d+)$", str(key))
            if not match:
                continue
            name = (
                value.get("Card Series")
                or value.get("Card Model")
                or value.get("Market Name")
                or ""
            )
            if isinstance(name, str) and name.strip():
                names[int(match.group(1))] = name.strip()
        return names
    except (OSError, subprocess.TimeoutExpired, ValueError, TypeError):
        return {}


def amd_linux_snapshot(sysfs_root: Path | None = None) -> dict:
    root = Path(sysfs_root) if sysfs_root is not None else Path("/sys")
    drm_dir = root / "class" / "drm"
    if not drm_dir.is_dir():
        return {"status": "unavailable", "reason": "drm_sysfs_unavailable", "devices": []}
    cards = sorted(
        p
        for p in drm_dir.iterdir()
        if p.name.startswith("card") and p.name[4:].isdigit()
    )
    rocm_names: dict[int, str] | None = None
    devices: list[dict] = []
    for card in cards:
        dev = card / "device"
        vendor = (_read_text(dev / "vendor") or "").lower()
        if vendor != "0x1002":
            continue
        if rocm_names is None:
            rocm_names = _rocm_smi_names() if sysfs_root is None else {}
        card_index = int(card.name[4:])
        pci_id = (_read_text(dev / "device") or "").lower()
        arch_family = _AMD_PCI_ARCH.get(pci_id)
        total = _positive_int(_read_text(dev / "mem_info_vram_total"))
        used = _nonnegative_int(_read_text(dev / "mem_info_vram_used"))
        free = total - used if total is not None and used is not None and used <= total else None
        missing: dict[str, str] = {}
        if total is None:
            missing["memory_total_bytes"] = "not_reported"
        if free is None:
            missing["memory_free_bytes"] = "not_reported"
        if arch_family is None:
            missing["arch_family"] = "not_reported"
        name = rocm_names.get(card_index) or (
            f"AMD Radeon ({pci_id})" if pci_id else "AMD Radeon GPU"
        )
        devices.append(
            {
                "index": card_index,
                "vendor": "amd",
                "name": name,
                "pci_device_id": pci_id or None,
                "arch_family": arch_family,
                "memory_kind": "dedicated" if total is not None else "unmeasured",
                "memory_total_bytes": total,
                "memory_free_bytes": free,
                "missing": missing,
            }
        )
    if not devices:
        return {"status": "unavailable", "reason": "no_amd_devices", "devices": []}
    return {"status": "observed", "source": "sysfs_drm", "devices": devices}


def _parse_windows_amd_adapters(stdout: str) -> list[dict]:
    if not stdout.strip():
        return []
    raw = json.loads(stdout)
    rows = raw if isinstance(raw, list) else [raw] if isinstance(raw, dict) else []
    devices: list[dict] = []
    for idx, row in enumerate(rows):
        if not isinstance(row, dict):
            continue
        name = str(row.get("Name") or "").strip()
        pnp = str(row.get("PNPDeviceID") or "").upper()
        lower = name.lower()
        if "VEN_1002" not in pnp and "amd" not in lower and "radeon" not in lower:
            continue
        driver = str(row.get("DriverVersion") or "").strip() or None
        pci_match = re.search(r"DEV_([0-9A-F]{4})", pnp)
        pci_id = f"0x{pci_match.group(1).lower()}" if pci_match else None
        arch_family = _AMD_PCI_ARCH.get(pci_id) if pci_id else None
        missing = {
            "memory_total_bytes": "windows_wmi_32bit_cap",
            "memory_free_bytes": "not_measured",
        }
        if arch_family is None:
            missing["arch_family"] = "not_reported"
        devices.append(
            {
                "index": idx,
                "vendor": "amd",
                "name": name or "AMD Radeon GPU",
                "driver_version": driver,
                "pci_device_id": pci_id,
                "arch_family": arch_family,
                "memory_kind": "unmeasured",
                "memory_total_bytes": None,
                "memory_free_bytes": None,
                "missing": missing,
            }
        )
    return devices


def amd_windows_snapshot() -> dict:
    powershell = shutil.which("powershell") or shutil.which("pwsh")
    if powershell is None:
        return {"status": "unavailable", "reason": "powershell_not_found", "devices": []}
    script = (
        "Get-CimInstance Win32_VideoController | "
        "Select-Object Name,DriverVersion,AdapterRAM,PNPDeviceID | "
        "ConvertTo-Json -Compress"
    )
    try:
        result = subprocess.run(
            [powershell, "-NoProfile", "-NonInteractive", "-Command", script],
            capture_output=True,
            text=True,
            timeout=6,
            creationflags=_no_window_flags(),
        )
        if result.returncode:
            return {"status": "unavailable", "reason": "windows_gpu_query_failed", "devices": []}
        devices = _parse_windows_amd_adapters(result.stdout)
        if not devices:
            return {"status": "unavailable", "reason": "no_amd_devices", "devices": []}
        return {"status": "observed", "source": "win32_video_controller", "devices": devices}
    except subprocess.TimeoutExpired:
        return {"status": "unavailable", "reason": "probe_timeout", "devices": []}
    except (OSError, UnicodeError, ValueError, TypeError):
        return {"status": "unavailable", "reason": "windows_gpu_query_failed", "devices": []}


def detect_gpus(system: str) -> dict:
    nv = nvidia_snapshot()
    amd = (
        amd_linux_snapshot()
        if system == "Linux"
        else amd_windows_snapshot()
        if system == "Windows"
        else {"status": "unavailable", "reason": "unsupported_platform", "devices": []}
    )
    nv_ok = nv.get("status") == "observed" and bool(nv.get("devices"))
    amd_ok = amd.get("status") == "observed" and bool(amd.get("devices"))
    if nv_ok and amd_ok:
        return {
            "status": "observed",
            "source": f"{nv['source']}+{amd['source']}",
            "devices": [*nv["devices"], *amd["devices"]],
        }
    if nv_ok:
        return nv
    if amd_ok:
        return amd
    return nv


def _linux_cpu_features(cpuinfo_path: Path | None = None) -> tuple[dict, str, dict]:
    text = _read_text(cpuinfo_path or Path("/proc/cpuinfo")) or ""
    for line in text.splitlines():
        key, sep, value = line.partition(":")
        if sep and key.strip() == "flags":
            flags = set(value.split())
            return (
                {"avx2": "avx2" in flags, "avx512": "avx512f" in flags},
                "/proc/cpuinfo:flags",
                {},
            )
    return (
        {"avx2": None, "avx512": None},
        "/proc/cpuinfo:flags",
        {"avx2": "not_reported", "avx512": "not_reported"},
    )


def _macos_cpu_features() -> tuple[dict, str, dict]:
    if platform.machine().lower() in ("arm64", "aarch64"):
        return (
            {"avx2": None, "avx512": None},
            "platform.machine",
            {"avx2": "not_applicable_arm64", "avx512": "not_applicable_arm64"},
        )
    features: dict[str, bool | None] = {}
    missing: dict[str, str] = {}
    for name, key in (("avx2", "hw.optional.avx2_0"), ("avx512", "hw.optional.avx512f")):
        try:
            res = subprocess.run(
                ["/usr/sbin/sysctl", "-n", key], capture_output=True, text=True, timeout=2
            )
            val = res.stdout.strip()
            if res.returncode == 0 and val in ("0", "1"):
                features[name] = val == "1"
            else:
                features[name] = None
                missing[name] = "not_reported"
        except (OSError, subprocess.TimeoutExpired):
            features[name] = None
            missing[name] = "query_failed"
    return features, "sysctl:hw.optional", missing


def _windows_cpu_features() -> tuple[dict, str, dict]:
    try:
        import ctypes

        kernel32 = ctypes.windll.kernel32
        avx2 = bool(kernel32.IsProcessorFeaturePresent(40))
        avx512 = bool(kernel32.IsProcessorFeaturePresent(41))
        return {"avx2": avx2, "avx512": avx512}, "kernel32:IsProcessorFeaturePresent", {}
    except (AttributeError, OSError):
        return (
            {"avx2": None, "avx512": None},
            "kernel32:IsProcessorFeaturePresent",
            {"avx2": "query_failed", "avx512": "query_failed"},
        )


def cpu_features_snapshot(system: str) -> tuple[dict, str, dict]:
    if system == "Linux":
        return _linux_cpu_features()
    if system == "Darwin":
        return _macos_cpu_features()
    if system == "Windows":
        return _windows_cpu_features()
    return (
        {"avx2": None, "avx512": None},
        "not_measured",
        {"avx2": "unsupported_platform", "avx512": "unsupported_platform"},
    )
