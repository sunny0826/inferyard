"""Read-only Windows single-GGUF service identity, using exact creation FILETIME."""

import hashlib
import os
import socket
import time
from pathlib import Path

import psutil

from inferyard.config.engine_fit_native_sources import (
    WINDOWS_LISTENER_SOURCE,
    WINDOWS_START_SOURCE,
)
from inferyard.contracts.validation import ContractError
from inferyard.platforms.identity import PreflightError, resolve_loopback_origin


def _remaining(deadline):
    if time.monotonic() >= deadline:
        raise PreflightError("engine_fit_windows_observation_timeout")


def _start(pid):
    from inferyard.platforms.windows_api import process_start

    value = process_start(pid)
    if type(value) is not int or not 0 < value <= 2**63 - 1:
        raise PreflightError("engine_fit_service_identity_unavailable")
    return value


def _listener(pid, address, port):
    family = socket.AF_INET6 if ":" in address else socket.AF_INET
    matches = []
    for row in psutil.net_connections(kind="tcp"):
        if row.status != psutil.CONN_LISTEN or row.laddr.port != port:
            continue
        if row.family == family and row.laddr.ip in (address, "::", "0.0.0.0"):
            matches.append(row)
        elif family == socket.AF_INET and row.family == socket.AF_INET6 and row.laddr.ip == "::":
            # IPv6 wildcard can also accept IPv4; its v6-only flag is not observed.
            matches.append(row)
    if len(matches) != 1 or matches[0].pid != pid or matches[0].family != family:
        raise PreflightError("endpoint_pid_mismatch")
    return f"windows:tcp:{address}:{port}:pid:{pid}"


def _state(pid, deadline):
    _remaining(deadline)
    start = _start(pid)
    process = psutil.Process(pid)
    current_user, owner = psutil.Process().username(), process.username()
    if (
        type(current_user) is not str
        or not current_user
        or type(owner) is not str
        or not owner
        or current_user.casefold() != owner.casefold()
    ):
        raise PreflightError("engine_fit_service_user_mismatch")
    arguments = process.cmdline()
    if (
        not arguments
        or len(arguments) > 1024
        or any(type(value) is not str or "\0" in value for value in arguments)
    ):
        raise PreflightError("engine_fit_arguments_unavailable")
    raw = b"\0".join(os.fsencode(value) for value in arguments) + b"\0"
    if len(raw) > 128 * 1024:
        raise PreflightError("engine_fit_arguments_unavailable")
    executable, cwd = Path(process.exe()), Path(process.cwd())
    if not executable.is_absolute() or not cwd.is_absolute():
        raise PreflightError("engine_fit_service_identity_unavailable")
    executable, cwd = executable.resolve(strict=True), cwd.resolve(strict=True)
    if not cwd.is_dir() or _start(pid) != start:
        raise PreflightError("engine_fit_service_identity_changed")
    _remaining(deadline)
    return start, arguments, raw, executable, cwd, owner


def _environment(pid):
    from inferyard.platforms.engine_fit_entrypoints import reject_llama_environment

    environment = psutil.Process(pid).environ()
    reject_llama_environment(environment)
    # Windows names are case-insensitive; values are never returned or persisted.
    if any(key.upper().startswith("LLAMA_ARG_") for key in environment):
        raise PreflightError("engine_fit_llama_environment_requires_explicit_args")


def bind_service(
    engine, model_path, pid, url, *, lms_path=None, models_root=None, served_model=None
):
    from inferyard.platforms.engine_fit import _file_hash, _stamp
    from inferyard.platforms.engine_fit_entrypoints import startup_binding, validate_model

    if engine != "llama-cpp" or type(pid) is not int or pid < 1:
        raise PreflightError("engine_fit_windows_engine_unavailable")
    if lms_path is not None or models_root is not None:
        raise PreflightError("engine_fit_lms_options_wrong_engine")
    deadline = time.monotonic() + 10.0
    try:
        origin, address, port = resolve_loopback_origin(url)
        state = _state(pid, deadline)
        start, arguments, raw, executable, cwd, _owner = state
        _environment(pid)
        startup, source, extras = startup_binding(engine, arguments, executable, cwd, windows=True)
        model = validate_model(startup, model_path, source)
        model_stat, executable_stat = model.stat(), executable.stat()
        digest, _size = _file_hash(executable)
        listener = _listener(pid, address, port)
        _environment(pid)
        if (
            _state(pid, deadline) != state
            or _stamp(executable.stat()) != _stamp(executable_stat)
            or (model.stat().st_dev, model.stat().st_ino) != (model_stat.st_dev, model_stat.st_ino)
            or not os.path.samefile(startup, model)
            or _listener(pid, address, port) != listener
        ):
            raise PreflightError("engine_fit_service_identity_changed")
        _remaining(deadline)
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
            "listener_source": WINDOWS_LISTENER_SOURCE,
            "process_start_source": WINDOWS_START_SOURCE,
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


def resource_snapshot(binding):
    from inferyard.platforms.engine_fit_windows_resources import resource_snapshot as snapshot

    return snapshot(binding)
