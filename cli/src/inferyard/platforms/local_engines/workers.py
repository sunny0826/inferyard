"""Identity covers live descendants without changing single-PID resource metrics."""

import ast
import hashlib
import os
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
                if permissions is None:
                    permissions = mapping_permissions.observe(pid)
                mapping_permissions.require_non_executable(permissions, path)
            elif native_image:
                raise PreflightError("local_engine_mapping_outside_runtime")
            continue
        mapped.append(known[stamp])
    verify_libraries([Path(item.path) for item in runtime_files], executable, mapped_paths=mapped)


def observe_workers(pid, psutil, runtime_files, environment):
    root = psutil.Process(pid)
    first = native._children(root)
    python_root = None
    bootstrap = str(files("inferyard").joinpath("data/local_engine/bootstrap.py"))
    if first and any(item.path == str(Path(bootstrap).resolve()) for item in runtime_files):
        root_state = native.service_process_state(pid, psutil)
        if root_state[1][1:5] == ["-I", "-S", "-B", bootstrap]:
            python_root = root_state
    pending, states, result = [(child, pid) for child in first], {}, []
    while pending:
        child, parent = pending.pop()
        if child in states or len(states) >= 10000 or child == pid:
            raise PreflightError("local_engine_worker_tree_changed")
        process = psutil.Process(child)
        state = native.service_process_state(child, psutil)
        start, _, raw, executable, cwd = state
        if process.ppid() != parent:
            raise PreflightError("local_engine_worker_tree_changed")
        observed_environment = process.environ()
        check_environment(observed_environment, environment)
        if python_root is not None:
            if executable != python_root[3]:
                raise PreflightError("local_engine_python_worker_executable_mismatch")
            validate_python_worker(state[1], python_root[1])
        children = native._children(process)
        states[child] = (parent, state, children)
        pending.extend((item, child) for item in children)
        try:
            stat = os.stat(executable)
        except OSError as exc:
            raise PreflightError("identity_file_unreadable") from exc
        identity = next(
            (
                item
                for item in runtime_files
                if item.path == str(Path(executable).resolve())
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
    if native._children(root) != first:
        raise PreflightError("local_engine_worker_tree_changed")
    for child, (parent, state, children) in states.items():
        process = psutil.Process(child)
        check_environment(process.environ(), environment)
        if (
            native.service_process_state(child, psutil) != state
            or process.ppid() != parent
            or native._children(process) != children
        ):
            raise PreflightError("local_engine_worker_tree_changed")
    return sorted(result, key=lambda row: row["pid"])
