"""Read-only identities and scoped resources for diagnostic engine comparisons."""

import hashlib
import ipaddress
import os
import platform
import re
import stat
import uuid
from pathlib import Path

from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import (
    PreflightError,
    memory_available,
    process_start_ticks,
    resolve_loopback_origin,
    verify_listener,
)


def _stamp(value):
    return (
        value.st_dev,
        value.st_ino,
        value.st_mode,
        value.st_size,
        value.st_mtime_ns,
        value.st_ctime_ns,
    )


def _file_hash(path):
    if os.name == "nt":
        from inferyard.platforms.engine_fit_windows_files import file_hash

        return file_hash(path)
    before, link = path.stat(), path.lstat()
    if not stat.S_ISREG(before.st_mode):
        raise PreflightError("engine_fit_not_regular_file")
    # Nonblocking open avoids hanging if a regular file is replaced by a FIFO.
    descriptor = os.open(path, os.O_RDONLY | getattr(os, "O_NONBLOCK", 0))
    with os.fdopen(descriptor, "rb") as stream:
        expected = _stamp(before)
        if _stamp(os.fstat(stream.fileno())) != expected:
            raise PreflightError("engine_fit_file_changed")
        digest = hashlib.file_digest(stream, "sha256").hexdigest()
        if _stamp(os.fstat(stream.fileno())) != expected:
            raise PreflightError("engine_fit_file_changed")
    if _stamp(path.stat()) != _stamp(before) or _stamp(path.lstat()) != _stamp(link):
        raise PreflightError("engine_fit_file_changed")
    return digest, before.st_size


def _inventory(root):
    result = {}
    pending = [root]
    while pending:
        directory = pending.pop()
        if directory.is_symlink():
            raise PreflightError("engine_fit_directory_symlink")
        result[directory.relative_to(root).as_posix()] = (
            _stamp(directory.lstat()),
            _stamp(directory.stat()),
        )
        for entry in sorted(directory.iterdir()):
            link, target = entry.lstat(), entry.stat()
            if stat.S_ISDIR(target.st_mode):
                if stat.S_ISLNK(link.st_mode):
                    raise PreflightError("engine_fit_directory_symlink")
                pending.append(entry)
            elif stat.S_ISREG(target.st_mode):
                result[entry.relative_to(root).as_posix()] = (_stamp(link), _stamp(target))
            else:
                raise PreflightError("engine_fit_not_regular_file")
    return result


def model_manifest(path: Path, *, definition=None) -> dict:
    """Hash a stable directory snapshot, following only regular-file symlinks."""
    try:
        path = Path(path)
        if path.is_symlink():
            raise PreflightError("engine_fit_directory_symlink")
        root = path.resolve(strict=True)
        if not root.is_dir():
            raise PreflightError("engine_fit_model_directory_required")
        inventory = _inventory
        if definition is not None:
            from inferyard.platforms.model_assets import DEFINITION, inventory

            if definition != DEFINITION:
                raise PreflightError("engine_fit_model_definition_unknown")
        before = inventory(root)
        files = []
        for relative, (_, target) in sorted(before.items()):
            if stat.S_ISREG(target[2]):
                digest, size = _file_hash(root / relative)
                files.append({"path": relative, "sha256": digest, "size": size})
        if not files:
            raise PreflightError("engine_fit_model_directory_empty")
        after = inventory(root, frozen_paths=before) if definition else inventory(root)
        if before != after:
            raise PreflightError("engine_fit_model_changed_during_hash")
        return {
            **({"definition": definition} if definition else {}),
            "path": str(root),
            "sha256": hashlib.sha256(json_bytes(files)).hexdigest(),
            "files": files,
        }
    except (OSError, ValueError, RuntimeError) as exc:
        if isinstance(exc, PreflightError):
            raise
        raise PreflightError("engine_fit_model_unreadable") from exc


def _machine_identity(system):
    if system == "Linux":
        for path in ("/etc/machine-id", "/var/lib/dbus/machine-id"):
            try:
                value = Path(path).read_text().strip()
                if re.fullmatch(r"[0-9a-fA-F]{32}", value) and int(value, 16):
                    return value.lower(), path
            except OSError, UnicodeError:
                pass
    elif system == "Darwin":
        from inferyard.platforms.macos_native import query

        output = query(["/usr/sbin/ioreg", "-rd1", "-c", "IOPlatformExpertDevice"]) or ""
        match = re.search(r'"IOPlatformUUID"\s*=\s*"([0-9A-Fa-f-]+)"', output)
        if match:
            try:
                identity = uuid.UUID(match[1])
                if identity.int:
                    return str(identity), "IOPlatformExpertDevice:IOPlatformUUID"
            except ValueError:
                pass
    elif system == "Windows":
        import winreg

        try:
            with winreg.OpenKey(
                winreg.HKEY_LOCAL_MACHINE,
                r"SOFTWARE\Microsoft\Cryptography",
                0,
                winreg.KEY_READ | winreg.KEY_WOW64_64KEY,
            ) as key:
                identity = uuid.UUID(winreg.QueryValueEx(key, "MachineGuid")[0])
                if identity.int:
                    return str(identity), "Windows_registry:MachineGuid"
        except OSError, ValueError:
            pass
    raise PreflightError("engine_fit_host_identity_unavailable")


def host_identity() -> dict:
    """Use OS machine identity and stable hardware, never hostname alone."""
    from inferyard.platforms.device_host import snapshot

    system = platform.system()
    identity, source = _machine_identity(system)
    if system == "Darwin":
        from inferyard.platforms.macos_native import sysctl_text

        try:
            hardware = {
                "cpu_model": sysctl_text("machdep.cpu.brand_string"),
                "logical_cpus": int(sysctl_text("hw.logicalcpu") or "0"),
                "memory_total_bytes": int(sysctl_text("hw.memsize") or "0"),
            }
        except ValueError as exc:
            raise PreflightError("engine_fit_host_hardware_unavailable") from exc
    else:
        hardware = snapshot(system)
    result = {"platform": system, "architecture": platform.machine(), "identity_source": source}
    for field in ("cpu_model", "logical_cpus", "memory_total_bytes"):
        result[field] = hardware.get(field)
        if not result[field]:
            raise PreflightError("engine_fit_host_hardware_unavailable")
    result["sha256"] = hashlib.sha256(json_bytes({**result, "machine_id": identity})).hexdigest()
    return result


def _linux(proc_root):
    if platform.system() != "Linux" and proc_root == Path("/proc"):
        raise PreflightError("engine_fit_live_requires_linux")


def _arguments(directory):
    raw = (directory / "cmdline").read_bytes()
    if not raw.endswith(b"\0") or not raw[:-1]:
        raise PreflightError("engine_fit_arguments_unavailable")
    return raw, [os.fsdecode(part) for part in raw[:-1].split(b"\0")]


def _startup_model(engine, arguments):
    cursor = 1
    while cursor < len(arguments) and arguments[cursor] in (
        "-u",
        "-B",
        "-E",
        "-I",
        "-s",
        "-S",
        "-P",
        "-O",
        "-OO",
    ):
        cursor += 1
    if engine == "vllm" and len(arguments) > 2 and arguments[1] == "serve":
        remaining = arguments[2:]
        if remaining[0].startswith("-") or any(
            item.split("=", 1)[0] in ("--model", "--config") for item in remaining[1:]
        ):
            raise PreflightError("engine_fit_model_argument_ambiguous")
        return remaining[0]
    if engine == "vllm" and len(arguments) > cursor + 2:
        if Path(arguments[cursor]).name == "vllm" and arguments[cursor + 1] == "serve":
            remaining = arguments[cursor + 2 :]
            if remaining[0].startswith("-") or any(
                item.split("=", 1)[0] in ("--model", "--config") for item in remaining[1:]
            ):
                raise PreflightError("engine_fit_model_argument_ambiguous")
            return remaining[0]
    module, option = {
        "vllm": ("vllm.entrypoints.openai.api_server", "--model"),
        "sglang": ("sglang.launch_server", "--model-path"),
    }[engine]
    if cursor == 0 or arguments[cursor : cursor + 2] != ["-m", module]:
        raise PreflightError("engine_fit_startup_entrypoint_unverified")
    remaining, values = arguments[cursor + 2 :], []
    for index, argument in enumerate(remaining):
        if argument == "--config" or argument.startswith("--config="):
            raise PreflightError("engine_fit_model_argument_ambiguous")
        if argument == option:
            if index + 1 == len(remaining):
                raise PreflightError("engine_fit_model_argument_ambiguous")
            values.append(remaining[index + 1])
        elif argument.startswith(option + "="):
            values.append(argument[len(option) + 1 :])
    if len(values) != 1 or not values[0] or values[0].startswith("-"):
        raise PreflightError("engine_fit_model_argument_ambiguous")
    return values[0]


def _listener(pid, address, port, proc_root):
    inode = verify_listener(pid, address, port, proc_root)
    ipv6 = ":" in address
    table = proc_root / str(pid) / "net" / ("tcp6" if ipv6 else "tcp")
    matches = []
    for line in table.read_text().splitlines()[1:]:
        fields = line.split()
        encoded, encoded_port = fields[1].split(":")
        raw = bytes.fromhex(encoded)
        raw = b"".join(raw[i : i + 4][::-1] for i in range(0, 16, 4)) if ipv6 else raw[::-1]
        host = str(ipaddress.ip_address(raw))
        if (
            fields[3] == "0A"
            and int(encoded_port, 16) == port
            and host
            in (
                address,
                "::" if ipv6 else "0.0.0.0",
            )
        ):
            matches.append(fields[9])
    if matches != [inode]:
        raise PreflightError("engine_fit_listener_ambiguous_or_changed")
    return inode


def bind_service(
    engine,
    model_path,
    pid,
    url,
    *,
    proc_root=Path("/proc"),
    lms_path=None,
    models_root=None,
    served_model=None,
) -> dict:
    """Bind a same-user native listener to an explicit supported startup entrypoint."""
    if platform.system() == "Windows" and proc_root == Path("/proc"):
        from inferyard.platforms.engine_fit_windows import bind_service as windows_bind

        return windows_bind(
            engine,
            model_path,
            pid,
            url,
            lms_path=lms_path,
            models_root=models_root,
            served_model=served_model,
        )
    if platform.system() == "Darwin" and proc_root == Path("/proc"):
        from inferyard.platforms.engine_fit_macos import bind_service as macos_bind

        return macos_bind(
            engine,
            model_path,
            pid,
            url,
            lms_path=lms_path,
            models_root=models_root,
            served_model=served_model,
        )
    _linux(proc_root)
    if lms_path is not None or models_root is not None:
        raise PreflightError("engine_fit_lms_options_wrong_engine")
    if engine not in ("vllm", "sglang", "llama-cpp") or type(pid) is not int or pid < 1:
        raise PreflightError("engine_fit_invalid_service")
    from inferyard.platforms.engine_fit_entrypoints import (
        linux_llama_environment,
        startup_binding,
        validate_model,
    )

    try:
        origin, address, port = resolve_loopback_origin(url)
        directory = proc_root / str(pid)
        if directory.stat().st_uid != os.getuid():
            raise PreflightError("engine_fit_service_user_mismatch")
        ticks = process_start_ticks(pid, proc_root)
        if engine == "llama-cpp":
            linux_llama_environment(directory)
        raw, arguments = _arguments(directory)
        startup_model, source, extras = startup_binding(
            engine,
            arguments,
            (directory / "exe").resolve(strict=True),
            (directory / "cwd").resolve(strict=True),
        )
        model = validate_model(startup_model, model_path, source)
        model_stat = model.stat()
        executable, _ = _file_hash(directory / "exe")
        listener = _listener(pid, address, port, proc_root)
        if engine == "llama-cpp":
            linux_llama_environment(directory)
        if process_start_ticks(pid, proc_root) != ticks or _arguments(directory)[0] != raw:
            raise PreflightError("engine_fit_service_identity_changed")
        return {
            "pid": pid,
            "start_ticks": ticks,
            "origin": origin,
            "address": address,
            "port": port,
            "executable_sha256": executable,
            "argv_sha256": hashlib.sha256(raw).hexdigest(),
            "listener_inode": listener,
            "model_binding": {
                "engine": engine,
                "path": str(model),
                "device": model_stat.st_dev,
                "inode": model_stat.st_ino,
                "source": source,
            },
            **extras,
        }
    except (OSError, ValueError, IndexError, ContractError) as exc:
        raise PreflightError("engine_fit_service_identity_unavailable") from exc


def check_service(binding, *, proc_root=Path("/proc")) -> None:
    observer = binding.get("observer", {})
    current = bind_service(
        binding["model_binding"]["engine"],
        binding["model_binding"]["path"],
        binding["pid"],
        binding["origin"],
        proc_root=proc_root,
        lms_path=observer.get("path"),
        models_root=observer.get("models_root"),
        served_model=observer.get("instance_id"),
    )
    if current != binding:
        raise PreflightError("engine_fit_service_identity_changed")


def _process_row(pid, proc_root):
    fields = (proc_root / str(pid) / "stat").read_text().rsplit(")", 1)[1].split()
    row = {
        key: int(fields[index])
        for key, index in (
            ("ppid", 1),
            ("start", 19),
            ("user", 11),
            ("system", 12),
            ("rss", 21),
        )
    }
    if min(row.values()) < 0 or fields[0] in ("Z", "X", "x"):
        raise PreflightError("engine_fit_process_counter_unavailable")
    return row


def _children(pid, proc_root):
    children = set()
    tasks = list((proc_root / str(pid) / "task").iterdir())
    if not tasks:
        raise PreflightError("engine_fit_process_tree_unavailable")
    for task in tasks:
        if task.name.isdigit():
            children.update(int(value) for value in (task / "children").read_text().split())
    return children


def _tree_counters(binding, proc_root):
    rows, children, pending = {}, {}, [(binding["pid"], None)]
    while pending:
        pid, parent = pending.pop()
        if pid in rows or len(rows) > 10000:
            raise PreflightError("engine_fit_process_tree_changed")
        row = _process_row(pid, proc_root)
        if (parent is None and row["start"] != binding["start_ticks"]) or (
            parent is not None and row["ppid"] != parent
        ):
            raise PreflightError("engine_fit_service_identity_changed")
        rows[pid], children[pid] = row, _children(pid, proc_root)
        pending.extend((child, pid) for child in children[pid])
    for pid, row in rows.items():
        after = _process_row(pid, proc_root)
        if any(after[key] != row[key] for key in ("start", "ppid")) or (
            _children(pid, proc_root) != children[pid]
        ):
            raise PreflightError("engine_fit_process_tree_changed")
    return rows


def resource_snapshot(binding, *, proc_root=Path("/proc")) -> dict:
    if platform.system() == "Windows" and proc_root == Path("/proc"):
        from inferyard.platforms.engine_fit_windows import (
            resource_snapshot as windows_resources,
        )

        return windows_resources(binding)
    if platform.system() == "Darwin" and proc_root == Path("/proc"):
        from inferyard.platforms.engine_fit_macos import resource_snapshot as macos_resources

        return macos_resources(binding)
    result = dict.fromkeys(
        (
            "memory_available_bytes",
            "process_tree_rss_bytes",
            "process_tree_cpu_seconds",
            "process_count",
        )
    )
    result.update(
        scope={
            "processes": "bound_pid_and_observed_live_descendants_via_proc_task_children",
            "rss": "/proc/PID/stat:rss_pages; summed_shared_pages_may_be_counted_more_than_once",
            "cpu": "/proc/PID/stat:utime+stime; cumulative_live_processes_excludes_exited_children",
            "memory_available": "/proc/meminfo:MemAvailable; host_not_model_attribution",
        },
        missing_reasons={},
    )
    try:
        _linux(proc_root)
        result["memory_available_bytes"] = memory_available(proc_root)
    except PreflightError as exc:
        result["missing_reasons"]["memory_available_bytes"] = str(exc)
    try:
        _linux(proc_root)
        rows = _tree_counters(binding, proc_root)
        tick_rate, page_size = os.sysconf("SC_CLK_TCK"), os.sysconf("SC_PAGE_SIZE")
        if min(tick_rate, page_size) <= 0:
            raise PreflightError("engine_fit_counter_scale_unavailable")
        result.update(
            process_tree_rss_bytes=sum(row["rss"] for row in rows.values()) * page_size,
            process_tree_cpu_seconds=sum(row["user"] + row["system"] for row in rows.values())
            / tick_rate,
            process_count=len(rows),
        )
        result["scope"].update(clock_ticks_per_second=tick_rate, page_size_bytes=page_size)
    except (OSError, ValueError, IndexError, PreflightError) as exc:
        reason = str(exc) if isinstance(exc, PreflightError) else "engine_fit_resource_unreadable"
        for field in ("process_tree_rss_bytes", "process_tree_cpu_seconds", "process_count"):
            result["missing_reasons"][field] = reason
    return result
