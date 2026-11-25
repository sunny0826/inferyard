"""Identity covers live descendants without changing single-PID resource metrics."""

import ast
import hashlib
import os
import time
from importlib.resources import files
from pathlib import Path

from inferyard.errors import PreflightError
from inferyard.platforms import engine_fit_macos as native
from inferyard.platforms.local_engines.closure import mapped_native_image, verify_libraries
from inferyard.platforms.local_engines.startup import check_environment
from inferyard.platforms.macos_process import lsof_records, mapped_file


_WORKER_ENTRYPOINTS = {
    "multiprocessing.spawn": "spawn_main",
    "multiprocessing.resource_tracker": "main",
    "multiprocessing.forkserver": "main",
}
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


def check_worker_env(
    observed_env,
    environment,
    *,
    allowed_extras=None,
    expected_lib_dirs=(),
    expected_cfg_env=None,
    check_lib_dirs=True,
):
    check_environment(observed_env, environment, allowed_extras=allowed_extras)
    if check_lib_dirs and expected_lib_dirs:
        actual_parts = observed_env.get("LD_LIBRARY_PATH", "").split(os.pathsep)
        if actual_parts[: len(expected_lib_dirs)] != list(expected_lib_dirs):
            raise PreflightError("local_engine_environment_override")
        if expected_cfg_env and "LD_LIBRARY_PATH" in expected_cfg_env:
            suffix = expected_cfg_env["LD_LIBRARY_PATH"].split(os.pathsep)
            if (
                actual_parts[len(expected_lib_dirs) : len(expected_lib_dirs) + len(suffix)]
                != suffix
            ):
                raise PreflightError("local_engine_environment_override")
    for key, expected_val in (expected_cfg_env or {}).items():
        if key == "LD_LIBRARY_PATH" and check_lib_dirs and expected_lib_dirs:
            continue
        if observed_env.get(key) != expected_val:
            raise PreflightError("local_engine_environment_override")


def validate_python_worker(arguments, root_arguments):
    """Recognize only isolated stdlib multiprocessing entrypoints or a fork."""
    if arguments == root_arguments:
        return
    if (
        len(arguments) < 6
        or arguments[0] != root_arguments[0]
        or sorted(arguments[1:4]) != ["-B", "-I", "-S"]
        or arguments[4] != "-c"
    ):
        raise PreflightError("local_engine_python_worker_isolation_required")
    try:
        body = ast.parse(arguments[5]).body
        if len(body) == 3 and ast.dump(body[0]) == ast.dump(ast.parse("import sys").body[0]):
            body = body[1:]
        if len(body) != 2 or not isinstance(body[0], ast.ImportFrom):
            raise ValueError
        imported, expression = body
        entry = _WORKER_ENTRYPOINTS.get(imported.module)
        if (
            entry is None
            or imported.level
            or len(imported.names) != 1
            or imported.names[0].name != entry
            or imported.names[0].asname is not None
            or not isinstance(expression, ast.Expr)
            or not isinstance(expression.value, ast.Call)
            or not isinstance(expression.value.func, ast.Name)
            or expression.value.func.id != entry
        ):
            raise ValueError
    except (SyntaxError, ValueError) as exc:
        raise PreflightError("local_engine_python_worker_entrypoint_unverified") from exc


def mapped_closure(rows, runtime_files, executable, *, pid=None):
    known = {(item.device, item.inode): Path(item.path) for item in runtime_files}
    mapped = []
    permissions = None
    for row in rows:
        if row.get("f") != "txt" or row.get("t") != "REG":
            continue
        try:
            stamp = int(row["D"], 16), int(row["i"])
            path = Path(row["n"])
            if not path.is_absolute():
                raise ValueError
        except (KeyError, ValueError, TypeError) as exc:
            raise PreflightError("local_engine_mapping_unverified") from exc
        if str(path).startswith(("/System/Library/", "/usr/lib/")):
            continue
        if stamp not in known:
            native_image = mapped_native_image(path, stamp)
            if native_image is None:
                from inferyard.platforms.local_engines import mapping_permissions

                if pid is None:
                    raise PreflightError("local_engine_mapping_unverified")
                if not mapping_permissions.cached_non_executable(pid, stamp, path):
                    if permissions is None:
                        permissions = mapping_permissions.observe(pid)
                    mapping_permissions.require_non_executable(permissions, path)
                    mapping_permissions.remember_non_executable(pid, stamp, path)
            elif native_image:
                raise PreflightError("local_engine_mapping_outside_runtime")
            continue
        mapped.append(known[stamp])
    verify_libraries([Path(item.path) for item in runtime_files], executable, mapped_paths=mapped)


def _observe_workers_once(
    pid,
    psutil,
    runtime_files,
    environment,
    *,
    family=None,
    allowed_extras=None,
    expected_lib_dirs=(),
    expected_cfg_env=None,
):
    root = psutil.Process(pid)
    first = native._children(root)
    python_root = None
    bootstrap = str(files("inferyard").joinpath("data/local_engine/bootstrap.py"))
    resolved_bootstrap = str(Path(bootstrap).resolve())
    if first and any(item.path == resolved_bootstrap for item in runtime_files):
        root_state = native.service_process_state(pid, psutil)
        if root_state[1][1:5] == ["-I", "-S", "-B", bootstrap]:
            python_root = root_state
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
        observed_environment = process.environ()
        check_worker_env(
            observed_environment,
            environment,
            allowed_extras=allowed_extras,
            expected_lib_dirs=expected_lib_dirs,
            expected_cfg_env=expected_cfg_env,
        )
        if python_root is not None:
            if executable != python_root[3]:
                raise PreflightError("local_engine_python_worker_executable_mismatch")
            validate_python_worker(arguments, python_root[1])
        states[child] = (parent, state, children)
        pending.extend((item, child) for item in children)
        try:
            stat = os.stat(executable)
        except OSError as exc:
            raise PreflightError("identity_file_unreadable") from exc
        resolved_executable = str(Path(executable).resolve())
        identity = next(
            (
                item
                for item in runtime_files
                if item.path == resolved_executable
                and item.device == stat.st_dev
                and item.inode == stat.st_ino
            ),
            None,
        )
        if identity is None:
            raise PreflightError("local_engine_worker_executable_outside_runtime")
        rows = lsof_records(child, [])
        if not mapped_file(child, identity, rows):
            raise PreflightError("local_engine_worker_executable_mapping_unverified")
        mapped_closure(rows, runtime_files, executable, pid=child)
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
        check_worker_env(
            process.environ(),
            environment,
            allowed_extras=allowed_extras,
            expected_lib_dirs=expected_lib_dirs,
            expected_cfg_env=expected_cfg_env,
        )
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
    runtime_files,
    environment,
    *,
    family=None,
    allowed_extras=None,
    expected_lib_dirs=(),
    expected_cfg_env=None,
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
                allowed_extras=allowed_extras,
                expected_lib_dirs=expected_lib_dirs,
                expected_cfg_env=expected_cfg_env,
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
