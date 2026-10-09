"""Read-only host identity checks. Never start, stop, or signal the measured service."""

from __future__ import annotations

import hashlib
import ipaddress
import os
import platform
import shutil
import socket
import stat
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit

from inferyard.config.loader import validate_endpoint
from inferyard.config.startup_arguments import model_argument
from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.platforms.cpu_policy import snapshot as cpu_policy_snapshot


class PreflightError(RuntimeError):
    """Fixed error category only; callers must not persist raw OS exceptions."""


@dataclass(frozen=True, slots=True)
class FileIdentity:
    path: str
    sha256: str
    size: int
    device: int
    inode: int
    mtime_ns: int

    def unchanged(self) -> bool:
        try:
            current = Path(self.path).stat()
            return (current.st_dev, current.st_ino, current.st_size, current.st_mtime_ns) == (
                self.device,
                self.inode,
                self.size,
                self.mtime_ns,
            )
        except OSError:
            return False


def hash_file(path: Path, expected: str | None = None) -> FileIdentity:
    try:
        with path.open("rb") as stream:
            before = os.fstat(stream.fileno())
            if not stat.S_ISREG(before.st_mode):
                raise PreflightError("identity_not_regular_file")
            digest = hashlib.file_digest(stream, "sha256").hexdigest()
            after = os.fstat(stream.fileno())
        result = FileIdentity(
            str(path.resolve()),
            digest,
            after.st_size,
            after.st_dev,
            after.st_ino,
            after.st_mtime_ns,
        )
        if (before.st_size, before.st_mtime_ns, before.st_ino) != (
            after.st_size,
            after.st_mtime_ns,
            after.st_ino,
        ) or not result.unchanged():
            raise PreflightError("identity_file_changed_during_hash")
        if expected is not None and digest != expected:
            raise PreflightError("identity_hash_mismatch")
        return result
    except OSError as exc:
        raise PreflightError("identity_file_unreadable") from exc


def process_start_ticks(pid: int, proc_root: Path = Path("/proc")) -> int:
    if platform.system() == "Darwin" and proc_root == Path("/proc"):
        from inferyard.platforms.macos_identity import process_start_ticks as macos_start

        return macos_start(pid)
    if os.name == "nt" and proc_root == Path("/proc"):
        from inferyard.platforms.windows_identity import process_start_ticks as windows_start

        return windows_start(pid)
    try:
        # comm is parenthesized and may itself contain spaces or ')'.
        fields = (proc_root / str(pid) / "stat").read_text().rsplit(")", 1)[1].split()
        return int(fields[19])  # field 22; fields starts at field 3 (state).
    except (OSError, ValueError, IndexError) as exc:
        raise PreflightError("service_process_unavailable") from exc


def memory_available(proc_root: Path = Path("/proc")) -> int:
    if platform.system() == "Darwin" and proc_root == Path("/proc"):
        from inferyard.platforms.macos_identity import memory_available as macos_memory

        return macos_memory()
    if os.name == "nt" and proc_root == Path("/proc"):
        from inferyard.platforms.windows_identity import memory_available as windows_memory

        return windows_memory()
    try:
        for line in (proc_root / "meminfo").read_text().splitlines():
            fields = line.split()
            if fields[0] == "MemAvailable:" and len(fields) == 3 and fields[2] == "kB":
                value = int(fields[1]) * 1024
                if value >= 0:
                    return value
    except OSError, ValueError, IndexError:
        pass
    raise PreflightError("system_memory_unavailable")


def _read_optional(path: Path):
    try:
        return path.read_text().strip()
    except OSError:
        return None


def environment_snapshot(proc_root: Path = Path("/proc"), sys_root: Path = Path("/sys")) -> dict:
    if platform.system() == "Darwin" and proc_root == Path("/proc") and sys_root == Path("/sys"):
        from inferyard.platforms.macos_identity import (
            environment_snapshot as macos_environment,
        )

        return macos_environment()
    if os.name == "nt" and proc_root == Path("/proc") and sys_root == Path("/sys"):
        from inferyard.platforms.windows_identity import (
            environment_snapshot as windows_environment,
        )

        return windows_environment()
    cpu = _read_optional(proc_root / "cpuinfo") or ""
    model = next(
        (
            line.split(":", 1)[1].strip()
            for line in cpu.splitlines()
            if line.startswith("model name")
        ),
        None,
    )
    mem = _read_optional(proc_root / "meminfo") or ""
    total = next(
        (int(line.split()[1]) * 1024 for line in mem.splitlines() if line.startswith("MemTotal:")),
        None,
    )
    vmstat = _read_optional(proc_root / "vmstat") or ""
    swap = {
        key: int(value)
        for line in vmstat.splitlines()
        for key, value in [line.split()]
        if key in ("pswpin", "pswpout")
    }
    power = {}
    for entry in sorted((sys_root / "class/power_supply").glob("*")):
        if _read_optional(entry / "type") in ("Mains", "USB", "USB_C"):
            power[entry.name] = _read_optional(entry / "online")
    cpufreq = sys_root / "devices/system/cpu/cpu0/cpufreq"
    try:
        release = platform.freedesktop_os_release()
        os_release = {
            key: release[key] for key in ("ID", "VERSION_ID", "BUILD_ID") if release.get(key)
        }
    except OSError:
        os_release = None
    flags = next(
        (line.split(":", 1)[1].split() for line in cpu.splitlines() if line.startswith("flags")),
        None,
    )
    try:
        available = memory_available(proc_root)
    except PreflightError:
        available = None
    return {
        "platform": platform.system(),
        "architecture": platform.machine(),
        "kernel": platform.release(),
        "os_release": os_release,
        "cpu_flags": flags,
        "scaling_driver": _read_optional(cpufreq / "scaling_driver"),
        "cpu_policies": cpu_policy_snapshot(sys_root),
        "cpu_model": model,
        "memory_total_bytes": total,
        "logical_cpus": os.cpu_count(),
        "mem_available_bytes": available,
        "ac_sources": power,
        "ac_online": any(v == "1" for v in power.values()) if power else None,
        "profile": _read_optional(sys_root / "firmware/acpi/platform_profile"),
        "governor": _read_optional(cpufreq / "scaling_governor"),
        "epp": _read_optional(cpufreq / "energy_performance_preference"),
        "swap_pages": swap,
        "page_size_bytes": os.sysconf("SC_PAGE_SIZE") if hasattr(os, "sysconf") else None,
        "gpu": {"backend": "not_used", "reason": "cpu_only_scope"},
    }


def resolve_loopback_origin(url: str) -> tuple[str, str, int]:
    """Resolve once and return a numeric origin to prevent DNS rebinding later."""
    validate_endpoint(url)
    parsed = urlsplit(url)
    port = parsed.port or (443 if parsed.scheme == "https" else 80)
    if parsed.hostname == "localhost":
        addresses = {
            item[4][0] for item in socket.getaddrinfo("localhost", port, type=socket.SOCK_STREAM)
        }
        if not addresses or any(not ipaddress.ip_address(a).is_loopback for a in addresses):
            raise PreflightError("localhost_resolved_outside_loopback")
        address = "127.0.0.1" if "127.0.0.1" in addresses else sorted(addresses)[0]
    else:
        address = parsed.hostname
    host = f"[{address}]" if ":" in address else address
    return f"{parsed.scheme}://{host}:{port}", address, port


def _socket_address(encoded: str, ipv6: bool) -> str:
    raw = bytes.fromhex(encoded)
    if ipv6:
        raw = b"".join(raw[i : i + 4][::-1] for i in range(0, 16, 4))
    else:
        raw = raw[::-1]
    return str(ipaddress.ip_address(raw))


def verify_listener(pid: int, address: str, port: int, proc_root: Path = Path("/proc")) -> str:
    if platform.system() == "Darwin" and proc_root == Path("/proc"):
        from inferyard.platforms.macos_identity import verify_listener as macos_listener

        return macos_listener(pid, address, port)
    if os.name == "nt" and proc_root == Path("/proc"):
        from inferyard.platforms.windows_identity import verify_listener as windows_listener

        return windows_listener(pid, address, port)
    try:
        owned = {
            target[8:-1]
            for entry in (proc_root / str(pid) / "fd").iterdir()
            if (target := os.readlink(entry)).startswith("socket:[")
        }
        ipv6 = ":" in address
        table = proc_root / str(pid) / "net" / ("tcp6" if ipv6 else "tcp")
        for line in table.read_text().splitlines()[1:]:
            fields = line.split()
            encoded, encoded_port = fields[1].split(":")
            host = _socket_address(encoded, ipv6)
            if (
                fields[3] == "0A"
                and int(encoded_port, 16) == port
                and host in (address, "::" if ipv6 else "0.0.0.0")
                and fields[9] in owned
            ):
                return fields[9]
    except (OSError, ValueError, IndexError) as exc:
        raise PreflightError("listener_identity_unavailable") from exc
    raise PreflightError("endpoint_pid_mismatch")


def sanitized_arguments(arguments: list[str]) -> list[str]:
    result = []
    skip = False
    for argument in arguments:
        if skip:
            skip = False
            continue
        option = argument.split("=", 1)[0]
        if option in ("--api-key", "--api-key-file", "--password", "--token", "-hft", "--hf-token"):
            skip = "=" not in argument
            continue
        result.append(argument)
    return result


def verify_process(
    config: dict,
    model: FileIdentity,
    engine: FileIdentity,
    address: str,
    port: int,
    proc_root: Path = Path("/proc"),
) -> dict:
    if config["engine"].get("adapter") in ("kvmem", "ninfer"):
        if platform.system() != "Windows":
            raise PreflightError("lab_adapter_requires_windows")
        from inferyard.platforms.lab_identity import verify_process as lab_process

        return lab_process(config, model, engine, address, port)
    if platform.system() == "Darwin" and proc_root == Path("/proc"):
        from inferyard.platforms.macos_identity import verify_process as macos_process

        return macos_process(config, model, engine, address, port)
    if os.name == "nt" and proc_root == Path("/proc"):
        from inferyard.platforms.windows_identity import verify_process as windows_process

        return windows_process(config, model, engine, address, port)
    pid = config["endpoint"]["server_pid"]
    expected_start = config["endpoint"]["process_start_ticks"]
    if process_start_ticks(pid, proc_root) != expected_start:
        raise PreflightError("service_process_identity_changed")
    directory = proc_root / str(pid)
    try:
        actual_exe = (directory / "exe").stat()
        if (actual_exe.st_dev, actual_exe.st_ino) != (engine.device, engine.inode):
            raise PreflightError("service_binary_mismatch")
        args = (directory / "cmdline").read_bytes().decode("utf-8").rstrip("\0").split("\0")[1:]
        if sanitized_arguments(args) != config["engine"]["startup_args"]:
            raise PreflightError("service_startup_arguments_mismatch")
        if config["engine"].get("slots_debug") is True:
            # Read only the one declared flag into evidence; never persist environ.
            values = [
                item.partition(b"=")[2]
                for item in (directory / "environ").read_bytes().split(b"\0")
                if item.partition(b"=")[0] == b"LLAMA_SERVER_SLOTS_DEBUG"
            ]
            if values != [b"1"]:
                raise PreflightError("service_slots_debug_environment_mismatch")
        mapped = False
        for line in (directory / "maps").read_text().splitlines():
            fields = line.split(maxsplit=5)
            major, minor = (int(part, 16) for part in fields[3].split(":"))
            if (int(fields[4]), major, minor) == (
                model.inode,
                os.major(model.device),
                os.minor(model.device),
            ):
                mapped = True
                break
        # Some CPU builds copy tensors then release file mappings. Require the exact
        # file inode in their verified startup arguments; /props is checked next.
        if not mapped:
            model_path = model_argument(args)
            if model_path is None:
                raise PreflightError("service_model_mapping_unverified")
            actual_model = Path(model_path).stat()
            if (actual_model.st_dev, actual_model.st_ino) != (model.device, model.inode):
                raise PreflightError("service_model_argument_mismatch")
        inode = verify_listener(pid, address, port, proc_root)
        if process_start_ticks(pid, proc_root) != expected_start:
            raise PreflightError("service_process_identity_changed")
        return {
            "pid": pid,
            "start_ticks": expected_start,
            "listener_inode": inode,
            "binary": "verified",
            "model_mapping": "verified" if mapped else "not_retained",
            "model_binding": "mapped_inode" if mapped else "verified_startup_file_inode",
            "endpoint": "verified",
            "startup_args": sanitized_arguments(args),
        }
    except (OSError, UnicodeError, ValueError, IndexError) as exc:
        raise PreflightError("service_identity_unreadable") from exc


def check_resources(config: dict, available: int, free_disk: int) -> None:
    if type(available) is not int:
        raise PreflightError("system_memory_unavailable")
    if available < config["output"]["min_available_memory_bytes"]:
        raise PreflightError("insufficient_available_memory")
    if free_disk < config["output"]["min_disk_bytes"]:
        raise PreflightError("insufficient_output_disk")


def static_preflight(
    config: dict, *, diagnostic=False, environment_policy=None
) -> tuple[dict, list[FileIdentity]]:
    system, backend = platform.system(), config["engine"].get("backend", "cpu")
    if system not in ("Linux", "Windows", "Darwin"):
        raise PreflightError("unsupported_platform")
    if (backend == "metal" and system != "Darwin") or (backend == "cuda" and system == "Darwin"):
        raise PreflightError("unsupported_backend_platform")
    origin, address, port = resolve_loopback_origin(config["endpoint"]["url"])
    if config["engine"].get("adapter") in ("kvmem", "ninfer"):
        if system != "Windows":
            raise PreflightError("lab_adapter_requires_windows")
        from inferyard.platforms.lab_identity import bind_files

        identities = bind_files(config)
    else:
        identities = [
            hash_file(Path(config["model"]["local_path"]), config["model"]["sha256"]),
            hash_file(Path(config["engine"]["binary_path"]), config["engine"]["binary_sha256"]),
            hash_file(Path(config["model"]["template_path"]), config["model"]["template_sha256"]),
        ]
        manifest_path = Path(config["engine"]["runtime_library_manifest"])
        try:
            manifest = strict_json_loads(manifest_path.read_text())
            if not isinstance(manifest, dict) or not manifest:
                raise PreflightError("invalid_library_manifest")
            for filename, digest in manifest.items():
                if (
                    Path(filename).name != filename
                    or not isinstance(digest, str)
                    or len(digest) != 64
                ):
                    raise PreflightError("invalid_library_manifest")
                identities.append(
                    hash_file(Path(config["engine"]["binary_path"]).parent / filename, digest)
                )
        except (OSError, ContractError) as exc:
            raise PreflightError("library_manifest_unreadable") from exc
    process = verify_process(config, identities[0], identities[1], address, port)
    environment = environment_snapshot()
    if backend == "metal":
        from inferyard.platforms.macos_identity import metal_capability

        environment.update(metal_capability())
    gpu = None
    if config["engine"].get("backend", "cpu") == "cuda":
        from inferyard.platforms.device_preflight import nvidia_snapshot

        gpu = nvidia_snapshot()
        if gpu["status"] != "observed":
            raise PreflightError("cuda_device_unavailable")
        environment["gpu"] = {"backend": "cuda", "source": "nvidia-smi", "devices": gpu["devices"]}
    root = Path(config["output"]["root"])
    ancestor = root
    while not ancestor.exists():
        ancestor = ancestor.parent
    check_resources(config, environment["mem_available_bytes"], shutil.disk_usage(ancestor).free)
    from inferyard.config.environment_binding import admission

    environment_admission = admission(
        config["conditions"], environment, diagnostic=diagnostic, policy=environment_policy
    )
    if environment_admission["blockers"]:
        raise PreflightError("frozen_environment_mismatch")
    if not all(item.unchanged() for item in identities):
        raise PreflightError("identity_file_changed")
    from inferyard.platforms.device_preflight import hardware_snapshot, recommend

    hardware = hardware_snapshot(
        ancestor, environment, **({"gpu_observation": gpu} if gpu is not None else {})
    )
    model = {
        "path": config["model"]["local_path"],
        "name": config["model"].get("display_name", "local"),
        "size_bytes": identities[0].size,
        "status": "available",
        "context_length": config["conditions"].get("context_size", 4096),
    }
    device_preflight = {
        "hardware": hardware,
        "recommendation": recommend(hardware, [model], model_loaded=True),
        "requested_backend": config["engine"].get("backend", "cpu"),
    }
    return {
        "origin": origin,
        "process": process,
        "environment": environment,
        "verification": "verified",
        "hashing_affects_page_cache": True,
        "environment_admission": environment_admission,
        "device_preflight": device_preflight,
    }, identities
