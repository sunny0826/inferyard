"""Identity covers live descendants without changing single-PID resource metrics."""

import ast
import hashlib
import os
from importlib.resources import files
from pathlib import Path

from inferyard.platforms import engine_fit_macos as native
from inferyard.platforms.identity import PreflightError, hash_file
from inferyard.platforms.local_engines.closure import mapped_native_image, verify_libraries
from inferyard.platforms.local_engines.startup import check_environment
from inferyard.platforms.macos_process import lsof_records, mapped_file


def validate_python_worker(arguments, root_arguments, import_roots):
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
        module = imported.module
        entry = "spawn_main" if module == "multiprocessing.spawn" else "main"
        if (
            imported.level
            or len(imported.names) != 1
            or imported.names[0].name != entry
            or imported.names[0].asname is not None
            or not isinstance(expression, ast.Expr)
            or not isinstance(expression.value, ast.Call)
            or not isinstance(expression.value.func, ast.Name)
            or expression.value.func.id != entry
        ):
            raise ValueError
        call = expression.value
        positional = [ast.literal_eval(node) for node in call.args]
        keywords = {}
        inherited_argv = False
        for keyword in call.keywords:
            if keyword.arg == "sys_argv":
                if ast.dump(keyword.value) != ast.dump(ast.parse("sys.argv[1:]", mode="eval").body):
                    raise ValueError
                inherited_argv = True
                continue
            value = ast.literal_eval(keyword.value)
            values = value if keyword.arg is None else {keyword.arg: value}
            if not isinstance(values, dict) or keywords.keys() & values.keys():
                raise ValueError
            keywords.update(values)

        def natural(value):
            return type(value) is int and value >= 0

        if module == "multiprocessing.spawn":
            if (
                positional
                or inherited_argv
                or set(keywords) != {"tracker_fd", "pipe_handle"}
                or not all(natural(value) for value in keywords.values())
                or arguments[6:] != ["--multiprocessing-fork"]
            ):
                raise ValueError
        elif module == "multiprocessing.resource_tracker":
            if (
                len(positional) != 1
                or not natural(positional[0])
                or keywords
                or inherited_argv
                or arguments[6:]
            ):
                raise ValueError
        elif module == "multiprocessing.forkserver":
            if (
                len(positional) != 3
                or not all(natural(value) for value in positional[:2])
                or positional[2] != ["__main__"]
                or set(keywords) - {"sys_path", "main_path", "authkey_r", "on_error"}
                or keywords.get("sys_path") != import_roots
                or ("authkey_r" in keywords and not natural(keywords["authkey_r"]))
                or keywords.get("on_error", "ignore") != "ignore"
                or (arguments[6:] and not inherited_argv)
                or (inherited_argv and arguments[7:] != root_arguments[6:])
            ):
                raise ValueError
            if "main_path" in keywords:
                script = str(files("inferyard").joinpath("data/local_engine/mlx_server.py"))
                if root_arguments[5] != "mlx-lm" or keywords["main_path"] != script:
                    raise ValueError
        else:
            raise ValueError
    except (SyntaxError, ValueError, TypeError, IndexError) as exc:
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
        root_state = native._state(pid, psutil)
        if root_state[1][1:5] == ["-I", "-S", "-B", bootstrap]:
            from inferyard.data.local_engine.bootstrap import import_paths
            from inferyard.platforms.local_engines.installation import _python

            python, _ = _python(
                {"executable": root_state[1][0], "working_directory": str(root_state[4])}
            )
            _, import_roots = import_paths(root_state[1][0], python["version"], python["path"])
            python_root = root_state
    pending, states, result = [(child, pid) for child in first], {}, []
    while pending:
        child, parent = pending.pop()
        if child in states or len(states) >= 10000 or child == pid:
            raise PreflightError("local_engine_worker_tree_changed")
        process = psutil.Process(child)
        state = native._state(child, psutil)
        start, _, raw, executable, cwd = state
        if process.ppid() != parent:
            raise PreflightError("local_engine_worker_tree_changed")
        observed_environment = process.environ()
        check_environment(observed_environment, environment)
        if python_root is not None:
            if executable != python_root[3]:
                raise PreflightError("local_engine_python_worker_executable_mismatch")
            validate_python_worker(state[1], python_root[1], import_roots)
        children = native._children(process)
        states[child] = (parent, state, children, observed_environment)
        pending.extend((item, child) for item in children)
        identity = hash_file(executable)
        if not any(
            item.path == identity.path and item.sha256 == identity.sha256 for item in runtime_files
        ):
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
    for child, (parent, state, children, observed_environment) in states.items():
        process = psutil.Process(child)
        if process.environ() != observed_environment:
            raise PreflightError("local_engine_service_changed")
        if (
            native._state(child, psutil) != state
            or process.ppid() != parent
            or native._children(process) != children
        ):
            raise PreflightError("local_engine_worker_tree_changed")
    return sorted(result, key=lambda row: row["pid"])
