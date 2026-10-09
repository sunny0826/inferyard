"""Read-only Windows process, listener and host identity; no service lifecycle actions."""

import os
import platform
import socket
import winreg
from pathlib import Path

import psutil

from inferyard.config.startup_arguments import model_argument
from inferyard.platforms.identity import PreflightError, sanitized_arguments
from inferyard.platforms.platform_io import durability
from inferyard.platforms.windows_api import process_start


def process_start_ticks(pid):
    try:
        return process_start(pid)
    except OSError as exc:
        # ERROR_INVALID_PARAMETER means that this PID does not exist. Access
        # denial is uncertainty, and must never authorize dirty-state recovery.
        reason = (
            "service_process_unavailable" if exc.winerror == 87 else "service_identity_unreadable"
        )
        raise PreflightError(reason) from exc


def process_arguments(pid):
    try:
        arguments = psutil.Process(pid).cmdline()
        if not arguments:
            raise PreflightError("service_identity_unreadable")
        return arguments
    except psutil.Error as exc:
        raise PreflightError("service_identity_unreadable") from exc


def memory_available():
    try:
        return psutil.virtual_memory().available
    except OSError as exc:
        raise PreflightError("system_memory_unavailable") from exc


def verify_listener(pid, address, port):
    family = socket.AF_INET6 if ":" in address else socket.AF_INET
    try:
        matches = [
            row
            for row in psutil.net_connections(kind="tcp")
            if row.family == family
            and row.status == psutil.CONN_LISTEN
            and row.laddr.port == port
            and row.laddr.ip in (address, "::" if family == socket.AF_INET6 else "0.0.0.0")
        ]
        if len(matches) == 1 and matches[0].pid == pid:
            # Windows has no socket inode; persist the actual ownership source.
            return f"windows:tcp:{address}:{port}:pid:{pid}"
    except (OSError, psutil.Error) as exc:
        raise PreflightError("listener_identity_unavailable") from exc
    raise PreflightError("endpoint_pid_mismatch")


def verify_process(config, model, engine, address, port):
    endpoint = config["endpoint"]
    pid, expected = endpoint["server_pid"], endpoint["process_start_ticks"]
    if process_start_ticks(pid) != expected:
        raise PreflightError("service_process_identity_changed")
    try:
        process = psutil.Process(pid)
        if not os.path.samefile(process.exe(), engine.path):
            raise PreflightError("service_binary_mismatch")
        args = process_arguments(pid)[1:]
        if sanitized_arguments(args) != config["engine"]["startup_args"]:
            raise PreflightError("service_startup_arguments_mismatch")
        if config["engine"].get("slots_debug") is True:
            if process.environ().get("LLAMA_SERVER_SLOTS_DEBUG") != "1":
                raise PreflightError("service_slots_debug_environment_mismatch")
        model_path = model_argument(args)
        if model_path is None:
            raise PreflightError("service_model_mapping_unverified")
        candidate = Path(model_path)
        if not candidate.is_absolute():
            candidate = Path(process.cwd()) / candidate
        if not os.path.samefile(candidate, model.path):
            raise PreflightError("service_model_argument_mismatch")
        listener = verify_listener(pid, address, port)
        if process_start_ticks(pid) != expected:
            raise PreflightError("service_process_identity_changed")
        gpu_backend = None
        if config["engine"].get("backend") == "cuda":
            cuda = Path(engine.path).parent / "ggml-cuda.dll"
            if not cuda.is_file() or not any(
                Path(mapping.path).name.lower() == "ggml-cuda.dll"
                and os.path.samefile(mapping.path, cuda)
                for mapping in process.memory_maps(grouped=True)
            ):
                raise PreflightError("cuda_backend_library_not_loaded")
            gpu_backend = {
                "backend": "cuda",
                "loaded_library": str(cuda),
                "source": "process_memory_maps",
            }
        return {
            "pid": pid,
            "start_ticks": expected,
            "process_start_source": "GetProcessTimes:creation_FILETIME_100ns_since_1601",
            "listener_inode": None,
            "listener_identity": listener,
            "listener_source": "GetExtendedTcpTable:owner_pid",
            "binary": "verified",
            "model_mapping": "not_observed",
            "model_binding": "verified_startup_file_identity",
            "endpoint": "verified",
            "startup_args": sanitized_arguments(args),
            **({"gpu_backend": gpu_backend} if gpu_backend else {}),
        }
    except (OSError, psutil.Error) as exc:
        raise PreflightError("service_identity_unreadable") from exc


def _cpu_model():
    try:
        with winreg.OpenKey(
            winreg.HKEY_LOCAL_MACHINE, r"HARDWARE\DESCRIPTION\System\CentralProcessor\0"
        ) as key:
            return winreg.QueryValueEx(key, "ProcessorNameString")[0].strip()
    except OSError:
        return None


def environment_snapshot():
    memory = psutil.virtual_memory()
    battery = psutil.sensors_battery()
    return {
        "platform": "Windows",
        "architecture": platform.machine(),
        "kernel": platform.release(),
        "os_release": {"ID": "Windows", "VERSION_ID": platform.version()},
        "cpu_flags": None,
        "scaling_driver": None,
        "cpu_policies": {"policies": [], "status": "unavailable"},
        "cpu_model": _cpu_model(),
        "memory_total_bytes": memory.total,
        "logical_cpus": os.cpu_count(),
        "mem_available_bytes": memory.available,
        "memory_source": "GlobalMemoryStatusEx:ullAvailPhys",
        "ac_sources": {},
        "ac_online": battery.power_plugged if battery is not None else None,
        "profile": None,
        "governor": None,
        "epp": None,
        "swap_pages": {},
        "page_size_bytes": None,
        "gpu": {"backend": "not_observed", "reason": "backend_requires_service_binding"},
        "evidence_durability": durability(),
        "limitations": ["windows_thermal_power_policy_and_swap_counters_unavailable"],
    }
