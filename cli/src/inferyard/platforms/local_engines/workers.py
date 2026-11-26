"""Live descendant process observation for safe process-tree lifecycle tracking."""

import hashlib
import os
import time
from pathlib import Path

from inferyard.errors import PreflightError
from inferyard.platforms import engine_fit_macos as native
from inferyard.platforms.identity import hash_file

_POLYSTRATA_MACOS_TELEMETRY_EXE = Path("/usr/sbin/ioreg")
_POLYSTRATA_MACOS_TELEMETRY_ARGS = ("-l", "-w0", "-r", "-c", "IOAccelerator")
_POLYSTRATA_WORKER_RETRY_ATTEMPTS = 3
_POLYSTRATA_WORKER_RETRY_SLEEP_SECONDS = 0.05
_POLYSTRATA_TRANSIENT_PREFLIGHT = frozenset(
    {
        "local_engine_worker_tree_changed",
        "service_process_unavailable",
        "service_identity_unreadable",
        "service_process_identity_changed",
    }
)


def _is_polystrata_macos_telemetry_probe(family, parent, root_pid, arguments, executable):
    if family != "polystrata" or parent != root_pid:
        return False
    if executable != _POLYSTRATA_MACOS_TELEMETRY_EXE:
        return False
    return (
        len(arguments) == 1 + len(_POLYSTRATA_MACOS_TELEMETRY_ARGS)
        and arguments[0] in ("ioreg", str(_POLYSTRATA_MACOS_TELEMETRY_EXE))
        and tuple(arguments[1:]) == _POLYSTRATA_MACOS_TELEMETRY_ARGS
    )


def _observe_workers_once(
    pid,
    psutil,
    _runtime_files=None,
    _environment=None,
    *,
    family=None,
):
    root = psutil.Process(pid)
    first = native._children(root)
    pending, states, ignored_probes, result = [(child, pid) for child in first], {}, set(), []
    while pending:
        child, parent = pending.pop()
        if child in states or child in ignored_probes or len(states) >= 10000 or child == pid:
            raise PreflightError("local_engine_worker_tree_changed")
        process = psutil.Process(child)
        state = native.service_process_state(child, psutil)
        start, arguments, raw, executable, cwd = state
        if process.ppid() != parent:
            raise PreflightError("local_engine_worker_tree_changed")
        children = native._children(process)
        if (
            _is_polystrata_macos_telemetry_probe(family, parent, pid, arguments, executable)
            and not children
        ):
            ignored_probes.add(child)
            continue
        states[child] = (parent, state, children)
        pending.extend((item, child) for item in children)
        identity = hash_file(Path(executable).resolve())
        result.append(
            {
                "pid": child,
                "parent_pid": parent,
                "start_ticks": start,
                "executable_sha256": identity.sha256,
                "argv_sha256": hashlib.sha256(raw).hexdigest(),
                "cwd_sha256": hashlib.sha256(os.fsencode(cwd)).hexdigest(),
            }
        )
    current_children = native._children(root)
    if family == "polystrata":
        extra_children = current_children - first
        for extra in extra_children:
            proc = psutil.Process(extra)
            extra_state = native.service_process_state(extra, psutil)
            if (
                proc.ppid() == pid
                and _is_polystrata_macos_telemetry_probe(
                    family, pid, pid, extra_state[1], extra_state[3]
                )
                and not native._children(proc)
            ):
                ignored_probes.add(extra)
        if (current_children - ignored_probes) != (first - ignored_probes):
            raise PreflightError("local_engine_worker_tree_changed")
    elif current_children != first:
        raise PreflightError("local_engine_worker_tree_changed")
    for child, (parent, state, children) in states.items():
        process = psutil.Process(child)
        if (
            native.service_process_state(child, psutil) != state
            or process.ppid() != parent
            or native._children(process) != children
        ):
            raise PreflightError("local_engine_worker_tree_changed")
    return sorted(result, key=lambda row: row["pid"])


def _process_alive(psutil, pid):
    try:
        return psutil.Process(pid).is_running()
    except psutil.Error:
        return False


def observe_workers(
    pid,
    psutil,
    runtime_files=None,
    environment=None,
    *,
    family=None,
    **_unused_kwargs,
):
    attempts = _POLYSTRATA_WORKER_RETRY_ATTEMPTS if family == "polystrata" else 1
    transient_psutil = (
        psutil.NoSuchProcess,
        getattr(psutil, "ZombieProcess", psutil.NoSuchProcess),
    )
    for attempt in range(attempts):
        try:
            return _observe_workers_once(
                pid,
                psutil,
                runtime_files,
                environment,
                family=family,
            )
        except PreflightError as exc:
            if (
                attempt + 1 >= attempts
                or str(exc) not in _POLYSTRATA_TRANSIENT_PREFLIGHT
                or not _process_alive(psutil, pid)
            ):
                raise
            time.sleep(_POLYSTRATA_WORKER_RETRY_SLEEP_SECONDS)
        except transient_psutil:
            if attempt + 1 >= attempts or not _process_alive(psutil, pid):
                raise
            time.sleep(_POLYSTRATA_WORKER_RETRY_SLEEP_SECONDS)
    raise AssertionError("unreachable")
