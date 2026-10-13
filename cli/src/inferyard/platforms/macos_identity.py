"""Native macOS process identity; never manage the external service lifecycle."""

import os
import platform
from pathlib import Path

from inferyard.config.startup_arguments import model_argument
from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.platforms.identity import PreflightError, hash_file, sanitized_arguments
from inferyard.platforms.macos_native import (
    boot_id,
    microseconds,
    psutil_module,
    query,
    swap_snapshot,
    sysctl_string,
    sysctl_text,
)
from inferyard.platforms.macos_process import lsof_records, mapped_file, matching_listener
from inferyard.platforms.platform_io import durability
from inferyard.platforms.power_macos import read_native


def process_start_ticks(pid):
    psutil = psutil_module()
    try:
        if type(pid) is not int or pid <= 0:
            raise PreflightError("service_process_unavailable")
        # psutil's public create_time() applies wall-clock corrections on Darwin.
        # Use the same raw kernel start time as psutil's own PID identity (7.2.2).
        # This private API is intentionally pinned and covered by native tests.
        return microseconds(psutil.Process(pid)._proc.create_time(monotonic=True))
    except psutil.NoSuchProcess as exc:
        raise PreflightError("service_process_unavailable") from exc
    except (AttributeError, OSError, ValueError, psutil.Error) as exc:
        # Permission failure is uncertainty, never proof of process disappearance.
        raise PreflightError("service_identity_unreadable") from exc


def process_arguments(pid):
    psutil = psutil_module()
    try:
        before = process_start_ticks(pid)
        arguments = psutil.Process(pid).cmdline()
        if not arguments or not all(isinstance(value, str) for value in arguments):
            raise PreflightError("service_identity_unreadable")
        if process_start_ticks(pid) != before:
            raise PreflightError("service_process_identity_changed")
        return arguments
    except (OSError, psutil.Error) as exc:
        raise PreflightError("service_identity_unreadable") from exc


def memory_available():
    psutil = psutil_module()
    try:
        value = psutil.virtual_memory().available
        if type(value) is not int or value < 0:
            raise ValueError
        return value
    except (OSError, ValueError, psutil.Error) as exc:
        raise PreflightError("system_memory_unavailable") from exc


def _listener_identity(pid, address, port, rows):
    matches = [row for row in rows if matching_listener(row, address, port)]
    if len(matches) != 1 or matches[0]["p"] != pid:
        raise PreflightError("endpoint_pid_mismatch")
    return f"macos:tcp:{address}:{port}:pid:{pid}"


def verify_listener(pid, address, port):
    before = process_start_ticks(pid)
    rows = lsof_records(pid, [f"-iTCP:{port}", "-sTCP:LISTEN"])
    if process_start_ticks(pid) != before:
        raise PreflightError("service_process_identity_changed")
    return _listener_identity(pid, address, port, rows)


def _model_argument(args, process, model):
    value = model_argument(args)
    if value is None:
        raise PreflightError("service_model_mapping_unverified")
    candidate = Path(value)
    if not candidate.is_absolute():
        candidate = Path(process.cwd()) / candidate
    observed = candidate.stat()
    if (observed.st_dev, observed.st_ino) != (model.device, model.inode):
        raise PreflightError("service_model_argument_mismatch")


def _bound_metal_library(bound_files, library):
    """Find the preflight identity for this library, including a resolved symlink."""
    for item in bound_files:
        try:
            if os.path.samefile(item.path, library):
                return item
        except OSError:
            continue
    return None


def _metal_binding(config, engine, pid, rows, bound_files=None):
    arguments = config["engine"]["startup_args"]
    options = ("-ngl", "--gpu-layers", "--n-gpu-layers")
    values = []
    for index, argument in enumerate(arguments):
        if argument in options and index + 1 < len(arguments):
            values.append(arguments[index + 1])
        elif argument.partition("=")[0] in options and "=" in argument:
            values.append(argument.partition("=")[2])
    if len(values) != 1 or not values[0].isdecimal() or int(values[0]) <= 0:
        raise PreflightError("metal_offload_configuration_unverified")
    library = Path(engine.path).parent / "libggml-metal.dylib"
    try:
        if bound_files is None:
            manifest = strict_json_loads(
                Path(config["engine"]["runtime_library_manifest"]).read_text()
            )
            if not isinstance(manifest, dict) or library.name not in manifest:
                raise PreflightError("metal_backend_library_not_bound")
            identity = hash_file(library, manifest[library.name])
        else:
            identity = _bound_metal_library(bound_files, library)
            if identity is None:
                raise PreflightError("metal_backend_library_not_bound")
        if not mapped_file(pid, identity, rows):
            raise PreflightError("metal_backend_library_not_loaded")
        if not identity.unchanged():
            raise PreflightError("identity_file_changed")
        return {
            "backend": "metal",
            "loaded_library": identity.path,
            "library_sha256": identity.sha256,
            "source": "lsof:txt:file_identity",
            "startup_offload_configuration_verified": True,
            "gpu_residency": "not_observed",
        }
    except (OSError, KeyError, ContractError) as exc:
        raise PreflightError("metal_backend_library_not_bound") from exc


def metal_capability():
    from inferyard.platforms.device_macos import displays

    if sysctl_text("hw.optional.arm64") != "1":
        raise PreflightError("metal_device_unavailable")
    text = query(
        [
            "/usr/sbin/system_profiler",
            "-json",
            "-detailLevel",
            "mini",
            "-timeout",
            "5",
            "SPDisplaysDataType",
        ],
        timeout=6,
    )
    gpu = displays(text, unified_memory=True)
    if gpu["status"] != "observed" or not any(
        row.get("metal_supported") is True and row.get("name", "").startswith("Apple")
        for row in gpu["devices"]
    ):
        raise PreflightError("metal_device_unavailable")
    return {"apple_silicon": True, "apple_gpu": gpu, "gpu": {"backend": "metal", **gpu}}


def verify_process(config, model, engine, address, port, *, bound_files=None):
    psutil = psutil_module()
    endpoint = config["endpoint"]
    pid, expected = endpoint["server_pid"], endpoint["process_start_ticks"]
    if process_start_ticks(pid) != expected:
        raise PreflightError("service_process_identity_changed")
    try:
        process = psutil.Process(pid)
        actual_exe = Path(process.exe()).stat()
        if (actual_exe.st_dev, actual_exe.st_ino) != (engine.device, engine.inode):
            raise PreflightError("service_binary_mismatch")
        # One fresh PID view per complete check; never retain it across calls.
        rows = lsof_records(pid, [])
        if process_start_ticks(pid) != expected:
            raise PreflightError("service_process_identity_changed")
        if not mapped_file(pid, engine, rows):
            raise PreflightError("service_binary_mapping_unverified")
        args = process_arguments(pid)[1:]
        if sanitized_arguments(args) != config["engine"]["startup_args"]:
            raise PreflightError("service_startup_arguments_mismatch")
        debug = config["engine"].get("slots_debug") is True
        if debug and process.environ().get("LLAMA_SERVER_SLOTS_DEBUG") != "1":
            raise PreflightError("service_slots_debug_environment_mismatch")
        _model_argument(args, process, model)
        listener = _listener_identity(pid, address, port, rows)
        metal = (
            _metal_binding(config, engine, pid, rows, bound_files)
            if config["engine"].get("backend") == "metal"
            else None
        )
        if process_start_ticks(pid) != expected:
            raise PreflightError("service_process_identity_changed")
        if not engine.unchanged() or not model.unchanged():
            raise PreflightError("identity_file_changed")
        return {
            "pid": pid,
            "start_ticks": expected,
            "process_start_source": "psutil:macos:raw_kernel_starttime:microseconds",
            "listener_inode": None,
            "listener_identity": listener,
            "listener_source": "lsof:TCP:LISTEN:pid",
            "binary": "verified",
            "model_mapping": "not_observed",
            "model_binding": "verified_startup_file_inode",
            "endpoint": "verified",
            "startup_args": sanitized_arguments(args),
            **({"slots_debug_environment_verified": True} if debug else {}),
            **({"gpu_backend": metal} if metal else {}),
        }
    except (OSError, psutil.Error) as exc:
        raise PreflightError("service_identity_unreadable") from exc


def environment_snapshot(constants=None):
    from inferyard.platforms.environment_constants import apply_constants, constant_value

    captured = constants or {}
    psutil = psutil_module()
    total = available = None
    try:
        memory = psutil.virtual_memory()
        total, available = memory.total, memory.available
    except OSError, psutil.Error:
        pass
    ac, power_policy = read_native()
    swap = swap_snapshot()
    if constants is not None:
        os_release = captured.get("os_release")
    else:
        os_release = {"ID": "macOS", "VERSION_ID": platform.mac_ver()[0]}
    return apply_constants(
        {
            "platform": "Darwin",
            "architecture": constant_value(constants, "architecture", platform.machine),
            "kernel": constant_value(constants, "kernel", platform.release),
            "os_release": os_release,
            "boot_id": constant_value(constants, "boot_id", boot_id),
            "cpu_flags": None,
            "scaling_driver": None,
            "cpu_policies": {"policies": [], "status": "unavailable"},
            "cpu_model": (
                captured.get("cpu_model")
                if constants is not None
                else sysctl_string("machdep.cpu.brand_string")
            ),
            "memory_total_bytes": total,
            "logical_cpus": constant_value(constants, "logical_cpus", os.cpu_count),
            "mem_available_bytes": available,
            "memory_source": "psutil:virtual_memory:available",
            "ac_sources": {"iokit:providing-power": "1" if ac else "0"} if ac is not None else {},
            "ac_online": ac,
            "profile": (
                f"macos-low-power:{power_policy['low_power_mode']}" if power_policy else None
            ),
            "macos_power_policy": power_policy,
            "governor": None,
            "epp": None,
            "swap_pages": {key: swap[key] for key in ("pswpin", "pswpout")},
            "swap_source": swap["source"],
            "page_size_bytes": swap["page_size_bytes"],
            "gpu": {"backend": "not_observed", "reason": "backend_requires_service_binding"},
            "evidence_durability": constant_value(constants, "evidence_durability", durability),
            "limitations": ["linux_cpufreq_not_applicable", "power_policy_is_sampled_not_atomic"],
        },
        constants,
    )
