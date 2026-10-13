"""Read-only Darwin engine identity and stable observed process-tree resources."""

import hashlib
import math
import os
import time
from pathlib import Path

from inferyard.contracts.validation import ContractError
from inferyard.platforms.engine_fit_macos_listener import unique_listener
from inferyard.platforms.identity import FileIdentity, PreflightError, service_origin
from inferyard.platforms.macos_identity import (
    _listener_identity,
    memory_available,
    process_arguments,
    process_start_ticks,
)
from inferyard.platforms.macos_native import psutil_module
from inferyard.platforms.macos_process import lsof_records, mapped_file


def _state(pid, psutil):
    start = process_start_ticks(pid)
    process = psutil.Process(pid)
    user = os.getuid()
    uids = process.uids()
    if (
        os.geteuid() != user
        or len(uids) != 3
        or any(type(value) is not int or value != user for value in uids)
    ):
        raise PreflightError("engine_fit_service_user_mismatch")
    arguments = process_arguments(pid)
    if any("\0" in value for value in arguments):
        raise PreflightError("engine_fit_arguments_unavailable")
    raw = b"\0".join(os.fsencode(value) for value in arguments) + b"\0"
    executable, cwd = Path(process.exe()), Path(process.cwd())
    if not executable.is_absolute() or not cwd.is_absolute():
        raise PreflightError("engine_fit_service_identity_unavailable")
    executable, cwd = executable.resolve(strict=True), cwd.resolve(strict=True)
    if not cwd.is_dir() or process_start_ticks(pid) != start:
        raise PreflightError("engine_fit_service_identity_changed")
    return start, arguments, raw, executable, cwd


def bind_service(
    engine,
    model_path,
    pid,
    url,
    *,
    lms_path=None,
    models_root=None,
    served_model=None,
):
    """Bind fresh PID, startup, mapped executable, model directory and TCP evidence."""
    from inferyard.platforms.engine_fit import _file_hash, _stamp
    from inferyard.platforms.engine_fit_entrypoints import (
        lmstudio_executable,
        macos_llama_environment,
        startup_binding,
        validate_model,
    )

    if (
        engine not in ("vllm", "sglang", "llama-cpp", "mlx-lm", "lmstudio")
        or type(pid) is not int
        or pid < 1
    ):
        raise PreflightError("engine_fit_invalid_service")
    if engine != "lmstudio" and (lms_path is not None or models_root is not None):
        raise PreflightError("engine_fit_lms_options_wrong_engine")
    psutil = psutil_module()
    deadline = time.monotonic() + 10.0
    try:
        origin, address, port = service_origin(url)
        state = _state(pid, psutil)
        start, arguments, raw, executable, cwd = state
        if engine == "llama-cpp":
            macos_llama_environment(pid, psutil)
        if engine == "lmstudio":
            from inferyard.platforms.engine_fit_lmstudio import bind_observer

            lmstudio_executable(executable)
            startup_model = Path(model_path).resolve(strict=True)
            source = "lms_loaded_instance_path"
            extras = {
                "observer": bind_observer(
                    lms_path,
                    models_root,
                    served_model,
                    startup_model,
                    address,
                    port,
                    deadline=deadline,
                )
            }
        else:
            startup_model, source, extras = startup_binding(engine, arguments, executable, cwd)
        model = validate_model(startup_model, model_path, source)
        model_stat = model.stat()
        before = executable.stat()
        digest, size = _file_hash(executable)
        identity = FileIdentity(
            str(executable), digest, size, before.st_dev, before.st_ino, before.st_mtime_ns
        )
        if engine == "lmstudio":
            from inferyard.platforms.engine_fit_lmstudio_files import lsof_records as lms_files
            from inferyard.platforms.engine_fit_observer_io import remaining

            rows = lms_files(pid, [], deadline=deadline)
        else:
            rows = lsof_records(pid, [])
        if not mapped_file(pid, identity, rows):
            raise PreflightError("engine_fit_executable_mapping_unverified")
        listener = _listener_identity(pid, address, port, rows)
        if engine == "lmstudio":
            unique_listener(pid, address, port, timeout=remaining(deadline))
        else:
            unique_listener(pid, address, port)
        if engine == "llama-cpp":
            macos_llama_environment(pid, psutil)
        if engine == "lmstudio":
            from inferyard.platforms.engine_fit_lmstudio_files import verify_model_file

            verify_model_file(pid, start, model, psutil=psutil, deadline=deadline)
        if engine == "mlx-lm" and startup_binding(engine, arguments, executable, cwd) != (
            startup_model,
            source,
            extras,
        ):
            raise PreflightError("engine_fit_service_identity_changed")
        final_model = model.stat()
        if (
            _state(pid, psutil) != state
            or _stamp(executable.stat()) != _stamp(before)
            or (final_model.st_dev, final_model.st_ino) != (model_stat.st_dev, model_stat.st_ino)
            or not os.path.samefile(startup_model, model)
        ):
            raise PreflightError("engine_fit_service_identity_changed")
        if engine == "lmstudio":
            remaining(deadline)
        return {
            "pid": pid,
            "start_ticks": start,
            "origin": origin,
            "address": address,
            "port": port,
            "executable_sha256": digest,
            "argv_sha256": hashlib.sha256(raw).hexdigest(),
            "cwd_sha256": hashlib.sha256(os.fsencode(cwd)).hexdigest(),
            "listener_inode": None,
            "listener_identity": listener,
            "listener_source": "lsof:TCP:LISTEN:pid",
            "model_binding": {
                "engine": engine,
                "path": str(model),
                "device": model_stat.st_dev,
                "inode": model_stat.st_ino,
                "source": source,
            },
            **extras,
        }
    except (OSError, ValueError, IndexError, ContractError, psutil.Error) as exc:
        raise PreflightError("engine_fit_service_identity_unavailable") from exc


def _children(process):
    children = [child.pid for child in process.children(recursive=False)]
    if any(type(pid) is not int or pid < 1 for pid in children) or len(children) != len(
        set(children)
    ):
        raise PreflightError("engine_fit_process_tree_changed")
    return set(children)


def _row(pid, psutil):
    start = process_start_ticks(pid)
    process = psutil.Process(pid)
    parent = process.ppid()
    cpu, rss = process.cpu_times(), process.memory_info().rss
    children = _children(process)
    if (
        type(parent) is not int
        or parent < 0
        or type(rss) is not int
        or rss < 0
        or any(
            type(value) not in (int, float) or not math.isfinite(value) or value < 0
            for value in (cpu.user, cpu.system)
        )
        or process.status() in (psutil.STATUS_ZOMBIE, psutil.STATUS_DEAD)
    ):
        raise PreflightError("engine_fit_process_counter_unavailable")
    if process_start_ticks(pid) != start or process.ppid() != parent:
        raise PreflightError("engine_fit_process_tree_changed")
    return {
        "start": start,
        "parent": parent,
        "rss": rss,
        "cpu": cpu.user + cpu.system,
        "children": children,
    }


def _tree_counters(binding, psutil):
    if any(type(binding[key]) is not int or binding[key] < 1 for key in ("pid", "start_ticks")):
        raise PreflightError("engine_fit_service_identity_changed")
    rows, pending = {}, [(binding["pid"], None)]
    while pending:
        pid, parent = pending.pop()
        if pid in rows or len(rows) >= 10000:
            raise PreflightError("engine_fit_process_tree_changed")
        row = _row(pid, psutil)
        if (parent is None and row["start"] != binding["start_ticks"]) or (
            parent is not None and row["parent"] != parent
        ):
            raise PreflightError("engine_fit_service_identity_changed")
        rows[pid] = row
        pending.extend((child, pid) for child in row["children"])
    for pid, row in rows.items():
        after = _row(pid, psutil)
        if any(after[key] != row[key] for key in ("start", "parent", "children")):
            raise PreflightError("engine_fit_process_tree_changed")
        if after["cpu"] < row["cpu"]:
            raise PreflightError("engine_fit_process_counter_unavailable")
    return rows


def resource_snapshot(binding):
    """Only stable live trees are summed; missing members never become partial sums."""
    fields = ("process_tree_rss_bytes", "process_tree_cpu_seconds", "process_count")
    result = dict.fromkeys(("memory_available_bytes", *fields))
    result.update(
        scope={
            "processes": "bound_pid_and_observed_live_descendants_via_psutil_Process_children",
            "rss": "psutil:Process.memory_info:rss_bytes; "
            "summed_shared_pages_may_be_counted_more_than_once",
            "cpu": "psutil:Process.cpu_times:user+system_seconds; "
            "cumulative_live_processes_excludes_exited_children",
            "memory_available": "psutil:virtual_memory:available_bytes; host_not_model_attribution",
            "process_start": "psutil:macos:raw_kernel_starttime:microseconds",
        },
        missing_reasons={},
    )
    try:
        result["memory_available_bytes"] = memory_available()
    except PreflightError as exc:
        result["missing_reasons"]["memory_available_bytes"] = str(exc)
    psutil = psutil_module()
    try:
        rows = _tree_counters(binding, psutil)
        cpu = sum(row["cpu"] for row in rows.values())
        if not math.isfinite(cpu):
            raise PreflightError("engine_fit_process_counter_unavailable")
        result.update(
            process_tree_rss_bytes=sum(row["rss"] for row in rows.values()),
            process_tree_cpu_seconds=cpu,
            process_count=len(rows),
        )
    except (OSError, ValueError, OverflowError, PreflightError, psutil.Error) as exc:
        reason = str(exc) if isinstance(exc, PreflightError) else "engine_fit_resource_unreadable"
        result["missing_reasons"].update(dict.fromkeys(fields, reason))
    return result
