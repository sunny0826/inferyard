"""Bind the actual Darwin listener, Python implementation and local model."""

import hashlib
import os
import platform
from collections.abc import Sequence
from pathlib import Path
from types import ModuleType

from inferyard.errors import PreflightError
from inferyard.platforms import engine_fit_macos as native
from inferyard.platforms.identity import (
    FileIdentity,
    hash_file,
    process_start_ticks,
    service_origin,
)
from inferyard.platforms.local_engines.assets import JsonObject, assets, bound_json
from inferyard.platforms.local_engines.installation import check_family_platform, darwin
from inferyard.platforms.local_engines.startup import check_environment, validate_startup
from inferyard.platforms.local_engines.workers import mapped_closure, observe_workers
from inferyard.platforms.macos_identity import _listener_identity
from inferyard.platforms.macos_process import lsof_records, mapped_file

DEFINITION = "local-engine-binding.v1"
_CACHE_LIMIT = 8

type Stamp = tuple[int, int, int, int, int]
type StampGroup = tuple[tuple[tuple[str, Stamp], ...], tuple[tuple[str, Stamp], ...]]
type AssetKey = tuple[str, str, str, str, str, str, str, str, str]
type CachedAssets = tuple[JsonObject, list[FileIdentity], list[FileIdentity], StampGroup]

_ASSET_CACHE: dict[AssetKey, CachedAssets] = {}


def _asset_key(config: JsonObject) -> AssetKey:
    engine, model = config["engine"], config["model"]
    return (
        engine["installation_path"],
        engine["installation_sha256"],
        model["local_path"],
        model["kind"],
        model["assets_path"],
        model["assets_sha256"],
        model["content_sha256"],
        model["template_path"],
        model["template_sha256"],
    )


def _stamp(path: Path | str) -> Stamp:
    info = Path(path).stat()
    return info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns


def _stamps(identities: Sequence[FileIdentity]) -> StampGroup | None:
    """Record ctimes and parent directories; FileIdentity alone misses both."""
    try:
        files = [(item.path, _stamp(item.path)) for item in identities]
        parents = sorted({str(Path(item.path).parent) for item in identities})
        directories = [(path, _stamp(path)) for path in parents]
    except OSError:
        return None
    return tuple(files), tuple(directories)


def _stamps_unchanged(stamps: StampGroup | None) -> bool:
    if not stamps:
        return False
    try:
        return all(_stamp(path) == expected for group in stamps for path, expected in group)
    except OSError:
        return False


def _remember(
    config: JsonObject,
    runtime: JsonObject,
    runtime_files: list[FileIdentity],
    asset_files: list[FileIdentity],
) -> None:
    identities = (*runtime_files, *asset_files)
    if not all(item.unchanged() for item in identities):
        return
    stamps = _stamps(identities)
    if stamps is None:
        return
    key = _asset_key(config)
    _ASSET_CACHE.pop(key, None)
    _ASSET_CACHE[key] = (runtime, runtime_files, asset_files, stamps)
    while len(_ASSET_CACHE) > _CACHE_LIMIT:
        _ASSET_CACHE.pop(next(iter(_ASSET_CACHE)))


def _cached(config: JsonObject) -> CachedAssets | None:
    value = _ASSET_CACHE.get(_asset_key(config))
    if value is None or not _stamps_unchanged(value[3]):
        return None
    return value


def validate_binding(binding: JsonObject, config: JsonObject) -> JsonObject:
    from inferyard.platforms.local_engines.binding_schema import validate

    return validate(binding, config)


def _binding_model(family: str, model: Path, info: os.stat_result) -> JsonObject:
    return {
        "engine": family,
        "path": str(model),
        "device": info.st_dev,
        "inode": info.st_ino,
        "source": "verified_startup_directory_identity"
        if model.is_dir()
        else "verified_startup_file_identity",
    }


def _direct(
    config: JsonObject,
    declaration: JsonObject,
    runtime_files: list[FileIdentity],
    psutil: ModuleType,
) -> JsonObject:
    endpoint = config["endpoint"]
    pid = endpoint["server_pid"]
    state = native.service_process_state(pid, psutil)
    start, arguments, raw, executable, cwd = state
    if start != endpoint["process_start_ticks"]:
        raise PreflightError("local_engine_pid_reused")
    validate_startup(config, declaration, arguments, executable, cwd)
    environment = psutil.Process(pid).environ()
    check_environment(environment, config["engine"]["environment"])
    identity = hash_file(executable)
    if not any(
        item.path == identity.path and item.sha256 == identity.sha256 for item in runtime_files
    ):
        raise PreflightError("local_engine_executable_outside_runtime")
    origin, address, port = service_origin(endpoint["url"])
    rows = lsof_records(pid, [])
    if not mapped_file(pid, identity, rows):
        raise PreflightError("local_engine_executable_mapping_unverified")
    mapped_closure(rows, runtime_files, executable, pid=pid)
    listener = _listener_identity(pid, address, port, rows)
    native.unique_listener(pid, address, port)
    model = Path(config["model"]["local_path"]).resolve(strict=True)
    info = model.stat()
    if native.service_process_state(pid, psutil) != state or psutil.Process(pid).environ() != environment:
        raise PreflightError("local_engine_service_changed")
    return {
        "pid": pid,
        "start_ticks": start,
        "origin": origin,
        "address": address,
        "port": port,
        "executable_sha256": identity.sha256,
        "argv_sha256": hashlib.sha256(raw).hexdigest(),
        "cwd_sha256": hashlib.sha256(os.fsencode(cwd)).hexdigest(),
        "listener_inode": None,
        "listener_identity": listener,
        "listener_source": "lsof:TCP:LISTEN:pid",
        "model_binding": _binding_model(config["engine"]["family"], model, info),
    }


def _require_family_platform(family: str) -> None:
    if family == "polystrata":
        check_family_platform(family)
    else:
        darwin()


def _psutil_process_state(pid: int, psutil: ModuleType) -> tuple[int, list[str], bytes, Path, Path]:
    proc = psutil.Process(pid)
    start = process_start_ticks(pid)
    arguments = list(proc.cmdline())
    raw = b"\0".join(os.fsencode(arg) for arg in arguments) + b"\0"
    executable = Path(proc.exe()).resolve(strict=True)
    cwd = Path(proc.cwd()).resolve(strict=True)
    return start, arguments, raw, executable, cwd


def _verify_same_user(proc, psutil: ModuleType) -> None:
    if platform.system() == "Windows":
        if hasattr(proc, "username"):
            current = psutil.Process().username()
            owner = proc.username()
            if (
                not isinstance(current, str)
                or not current
                or not isinstance(owner, str)
                or not owner
                or current.casefold() != owner.casefold()
            ):
                raise PreflightError("local_engine_service_user_mismatch")
        return
    if hasattr(proc, "uids") and hasattr(os, "getuid"):
        uids = proc.uids()
        if getattr(uids, "real", None) != os.getuid():
            raise PreflightError("local_engine_service_user_mismatch")


def _psutil_listener_identity(
    psutil: ModuleType, proc, pid: int, address: str, port: int
) -> str:
    wildcards = (address, "::") if ":" in address else (address, "0.0.0.0", "::")
    system_getter = getattr(psutil, "net_connections", None)
    if callable(system_getter):
        try:
            all_conns = system_getter(kind="tcp")
        except (OSError, PermissionError, psutil.Error) as exc:
            raise PreflightError("local_engine_listener_unverifiable") from exc
        matches = [
            conn
            for conn in all_conns
            if getattr(conn, "status", None) == "LISTEN"
            and conn.laddr
            and conn.laddr.port == port
            and conn.laddr.ip in wildcards
        ]
        if len(matches) != 1 or getattr(matches[0], "pid", None) != pid:
            raise PreflightError("local_engine_listener_mismatch")
        return f"tcp://{address}:{port}/pid:{pid}"
    raise PreflightError("local_engine_listener_unverifiable")


def _polystrata_worker_env_policy(
    config: JsonObject,
) -> tuple[frozenset[str], list[str], dict[str, str]]:
    if config["engine"].get("family") != "polystrata":
        return frozenset(), [], {}
    cfg_path = config["engine"].get("polystrata_config_path")
    if not cfg_path:
        return frozenset(), [], {}
    try:
        from inferyard.contracts.validation import strict_json_loads

        payload = strict_json_loads(Path(cfg_path).read_text(encoding="utf-8"))
    except (OSError, ValueError, UnicodeError):
        return frozenset(), [], {}
    if not isinstance(payload, dict):
        return frozenset(), [], {}
    allowed: set[str] = set()
    lib_dirs = [
        str(Path(d))
        for d in (payload.get("lib_dirs") or ())
        if isinstance(d, str) and d.strip() and Path(d).is_dir()
    ]
    if lib_dirs:
        allowed.add("LD_LIBRARY_PATH")
    if payload.get("backend") == "hip":
        allowed.add("HIP_VISIBLE_DEVICES")
    if payload.get("gpu") is not None or config["engine"].get("backend") == "cuda":
        allowed.update({"CUDA_VISIBLE_DEVICES", "CUDA_DEVICE_ORDER"})
    expected_env: dict[str, str] = {}
    if isinstance(payload.get("env"), dict):
        for k, v in payload["env"].items():
            key_s = str(k)
            allowed.add(key_s)
            expected_env[key_s] = str(v)
    return frozenset(allowed), lib_dirs, expected_env


def _direct_cross_platform(
    config: JsonObject,
    declaration: JsonObject,
    runtime_files: list[FileIdentity],
    psutil: ModuleType,
) -> JsonObject:
    endpoint = config["endpoint"]
    pid = endpoint["server_pid"]
    proc = psutil.Process(pid)
    _verify_same_user(proc, psutil)
    state = _psutil_process_state(pid, psutil)
    start, arguments, raw, executable, cwd = state
    if start != endpoint["process_start_ticks"]:
        raise PreflightError("local_engine_pid_reused")
    validate_startup(config, declaration, arguments, executable, cwd)
    environment = proc.environ()
    check_environment(environment, config["engine"]["environment"])
    identity = hash_file(executable)
    if not any(
        item.path == identity.path and item.sha256 == identity.sha256 for item in runtime_files
    ):
        raise PreflightError("local_engine_executable_outside_runtime")
    origin, address, port = service_origin(endpoint["url"])
    listener = _psutil_listener_identity(psutil, proc, pid, address, port)
    model = Path(config["model"]["local_path"]).resolve(strict=True)
    info = model.stat()
    if _psutil_process_state(pid, psutil) != state or proc.environ() != environment:
        raise PreflightError("local_engine_service_changed")
    return {
        "pid": pid,
        "start_ticks": start,
        "origin": origin,
        "address": address,
        "port": port,
        "executable_sha256": identity.sha256,
        "argv_sha256": hashlib.sha256(raw).hexdigest(),
        "cwd_sha256": hashlib.sha256(os.fsencode(cwd)).hexdigest(),
        "listener_inode": None,
        "listener_identity": listener,
        "listener_source": "psutil:TCP:LISTEN:pid",
        "model_binding": _binding_model(config["engine"]["family"], model, info),
    }


def _observe_workers_cross_platform(
    pid: int,
    psutil: ModuleType,
    runtime_files: list[FileIdentity],
    environment: dict,
    *,
    config: JsonObject,
) -> list[JsonObject]:
    root = psutil.Process(pid)
    children = root.children(recursive=True)
    allowed_extras, expected_lib_dirs, expected_cfg_env = _polystrata_worker_env_policy(config)
    seen_pids: set[int] = set()
    rows: list[JsonObject] = []
    for child_proc in children:
        child = child_proc.pid
        if child in seen_pids or child == pid:
            raise PreflightError("local_engine_worker_tree_changed")
        seen_pids.add(child)
        _verify_same_user(child_proc, psutil)
        parent = child_proc.ppid()
        start, _, raw, executable, cwd = _psutil_process_state(child, psutil)
        observed_env = child_proc.environ()
        check_environment(observed_env, environment, allowed_extras=allowed_extras)
        if expected_lib_dirs and platform.system() != "Windows":
            actual_parts = observed_env.get("LD_LIBRARY_PATH", "").split(os.pathsep)
            if actual_parts[: len(expected_lib_dirs)] != expected_lib_dirs:
                raise PreflightError("local_engine_environment_override")
            if "LD_LIBRARY_PATH" in expected_cfg_env:
                suffix = expected_cfg_env["LD_LIBRARY_PATH"].split(os.pathsep)
                if actual_parts[len(expected_lib_dirs) : len(expected_lib_dirs) + len(suffix)] != suffix:
                    raise PreflightError("local_engine_environment_override")
        for key, expected_val in expected_cfg_env.items():
            if key == "LD_LIBRARY_PATH" and expected_lib_dirs and platform.system() != "Windows":
                continue
            if observed_env.get(key) != expected_val:
                raise PreflightError("local_engine_environment_override")
        identity = hash_file(executable)
        if not any(
            item.path == identity.path and item.sha256 == identity.sha256
            for item in runtime_files
        ):
            raise PreflightError("local_engine_worker_executable_outside_runtime")
        rows.append(
            {
                "pid": child,
                "parent_pid": parent,
                "start_ticks": start,
                "executable_sha256": identity.sha256,
                "argv_sha256": hashlib.sha256(raw).hexdigest(),
                "cwd_sha256": hashlib.sha256(os.fsencode(cwd)).hexdigest(),
            }
        )
    return sorted(rows, key=lambda row: row["pid"])


def _bind(
    config: JsonObject, runtime: JsonObject, runtime_files: list[FileIdentity]
) -> JsonObject:
    declaration, engine, endpoint = runtime["installation"], config["engine"], config["endpoint"]
    worker_environment = declaration.get("worker_environment", engine["environment"])
    psutil = native.psutil_module()
    use_darwin_probe = platform.system() == "Darwin" or engine["family"] != "polystrata"
    try:
        if engine["family"] == "lmstudio":
            service = native.bind_service(
                "lmstudio",
                config["model"]["local_path"],
                endpoint["server_pid"],
                endpoint["url"],
                lms_path=declaration["executable"],
                models_root=declaration["models_root"],
                served_model=engine["served_model"],
            )
            if service["start_ticks"] != endpoint["process_start_ticks"]:
                raise PreflightError("local_engine_pid_reused")
            state = native.service_process_state(endpoint["server_pid"], psutil)
            if state[1] != engine["startup_args"] or str(state[4]) != engine["working_directory"]:
                raise PreflightError("local_engine_startup_mismatch")
            check_environment(
                psutil.Process(endpoint["server_pid"]).environ(), engine["environment"]
            )
            mapped_closure(
                lsof_records(endpoint["server_pid"], []),
                runtime_files,
                state[3],
                pid=endpoint["server_pid"],
            )
        elif use_darwin_probe:
            service = _direct(config, declaration, runtime_files, psutil)
        else:
            service = _direct_cross_platform(config, declaration, runtime_files, psutil)
        if use_darwin_probe:
            allowed_extras, expected_lib_dirs, expected_cfg_env = _polystrata_worker_env_policy(
                config
            )
            service["workers"] = observe_workers(
                endpoint["server_pid"],
                psutil,
                runtime_files,
                worker_environment,
                family=engine["family"],
                allowed_extras=allowed_extras,
                expected_lib_dirs=expected_lib_dirs,
                expected_cfg_env=expected_cfg_env,
            )
        else:
            service["workers"] = _observe_workers_cross_platform(
                endpoint["server_pid"], psutil, runtime_files, worker_environment, config=config
            )
        result = {
            "schema_version": 3,
            "definition": DEFINITION,
            "installation_sha256": engine["installation_sha256"],
            "service": service,
        }
        validate_binding(result, config)
        return result
    except (OSError, ValueError, IndexError, psutil.Error) as exc:
        raise PreflightError("local_engine_service_unavailable") from exc


def bind_service(config: JsonObject) -> JsonObject:
    _require_family_platform(config["engine"]["family"])
    runtime, runtime_files, asset_files = assets(config)
    result = _bind(config, runtime, runtime_files)
    _remember(config, runtime, runtime_files, asset_files)
    return result


def verify_bound_service(
    config: JsonObject, *, bound_files: Sequence[FileIdentity] | None = None
) -> JsonObject:
    endpoint = config["endpoint"]
    binding, _ = bound_json(endpoint["binding_path"], endpoint["binding_sha256"])
    validate_binding(binding, config)
    if bound_files is not None and any(not item.unchanged() for item in bound_files):
        raise PreflightError("local_engine_bound_file_changed")
    if bound_files is None:
        current = bind_service(config)
    else:
        _require_family_platform(config["engine"]["family"])
        cached = _cached(config)
        if cached is None:
            runtime, runtime_files, asset_files = assets(config)
            _remember(config, runtime, runtime_files, asset_files)
        else:
            runtime, runtime_files, _, _ = cached
        current = _bind(config, runtime, runtime_files)
    if current != binding:
        raise PreflightError("local_engine_bound_service_changed")
    return current["service"]
