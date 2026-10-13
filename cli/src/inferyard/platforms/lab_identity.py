"""Formal Windows lab binding: native stamps, same-handle process checks and DLLs."""

import time
from dataclasses import dataclass
from pathlib import Path

import psutil

from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import read_json
from inferyard.platforms.identity import PreflightError, sanitized_arguments
from inferyard.platforms.windows_lab_api import current_windows_lab_api
from inferyard.platforms.windows_lab_checks import (
    argv_digest,
    cwd_digest,
    same_windows_path,
    windows_file_ancestors,
)
from inferyard.platforms.windows_lab_files import manifest_hash_seconds, verify_file_manifest
from inferyard.platforms.windows_lab_process import verify_process_binding


@dataclass(frozen=True)
class LabFileIdentity:
    path: str
    sha256: str
    size: int
    device: int
    inode: int
    mtime_ns: int
    native_stamp: list
    role: str

    def unchanged(self):
        try:
            api = current_windows_lab_api()
            for path in windows_file_ancestors(self.path):
                api.reject_reparse(path)
            current = api.path_stamp(self.path)
            volume, identifier, *rest = current
            return [volume, identifier.hex(), *rest] == self.native_stamp
        except OSError, PreflightError:
            return False


def bind_files(config):
    entries = config["engine"]["asset_manifest"]
    # Large-model hashing has a fixed preparation deadline, never refreshed per file.
    verified = verify_file_manifest(
        entries, deadline=time.monotonic() + manifest_hash_seconds(entries)
    )
    order = {"model": 0, "engine": 1, "template": 2, "component_ledger": 3, "library": 4}
    verified.sort(key=lambda row: order[row["role"]])
    files = [
        LabFileIdentity(
            row["path"],
            row["sha256"],
            row["bytes"],
            row["native_stamp"][0],
            int(row["native_stamp"][1], 16),
            row["native_stamp"][5] * 100,
            row["native_stamp"],
            row["role"],
        )
        for row in verified
    ]
    try:
        manifest = read_json(Path(config["engine"]["runtime_library_manifest"]))
        libraries = {
            Path(row["path"]).name: row["sha256"] for row in entries if row["role"] == "library"
        }
        if manifest != libraries:
            raise PreflightError("lab_library_manifest_mismatch")
    except (OSError, ContractError) as exc:
        raise PreflightError("lab_library_manifest_invalid") from exc
    from inferyard.platforms.lab_assets import inspect_asset

    inspect_asset(config)
    if not all(file.unchanged() for file in files):
        raise PreflightError("identity_file_changed")
    return files


def verify_process(config, model, engine, address, port):
    endpoint = config["endpoint"]
    settings = config["engine"]
    expected = {
        "pid": endpoint["server_pid"],
        "creation_filetime": endpoint["process_start_ticks"],
        "origin": f"{'https' if endpoint['url'].startswith('https:') else 'http'}://"
        f"{'[' + address + ']' if ':' in address else address}:{port}",
        "executable_path": engine.path,
        "executable_sha256": engine.sha256,
        "argv_sha256": argv_digest([engine.path, *settings["startup_args"]]),
        "cwd_sha256": cwd_digest(settings["working_directory"]),
    }
    first = verify_process_binding(
        expected,
        deadline=time.monotonic() + 5,
        bound_executable=engine,
        inspect_details=lambda: _process_details(config, model, engine),
    )
    args = first.pop("details")
    return {
        **first,
        "pid": endpoint["server_pid"],
        "start_ticks": endpoint["process_start_ticks"],
        "binary": "verified",
        "model_mapping": "not_observed",
        "model_binding": "verified_startup_native_file_identity",
        "endpoint": "verified",
        "startup_args": sanitized_arguments(args),
        "engine_environment": "verified_cuda_device_cache_overrides",
    }


def _process_details(config, model, engine):
    endpoint, settings = config["endpoint"], config["engine"]
    try:
        process = psutil.Process(endpoint["server_pid"])
        args = process.cmdline()[1:]
        if sanitized_arguments(args) != settings["startup_args"]:
            raise PreflightError("service_startup_arguments_mismatch")
        models = [args[i + 1] for i, value in enumerate(args[:-1]) if value in ("-m", "--model")]
        models.extend(
            value.split("=", 1)[1] for value in args if value.startswith(("--model=", "-m="))
        )
        if settings["adapter"] == "ninfer" and args and not args[0].startswith("-"):
            models.insert(0, args[0])
        if len(models) != 1:
            raise PreflightError("service_model_mapping_unverified")
        candidate = Path(models[0])
        if not candidate.is_absolute():
            candidate = Path(settings["working_directory"]) / candidate
        api = current_windows_lab_api()
        stamp = api.path_stamp(str(candidate))
        if (stamp[0], int.from_bytes(stamp[1], "big")) != (
            model.device,
            model.inode,
        ) or not model.unchanged():
            raise PreflightError("service_model_argument_mismatch")
        observed_env = {
            key: value
            for key, value in process.environ().items()
            if key in ("CUDA_VISIBLE_DEVICES", "CUDA_CACHE_DISABLE")
        }
        if observed_env != settings.get("environment", {}):
            raise PreflightError("lab_unbound_engine_environment")
        declared = [row for row in settings["asset_manifest"] if row["role"] == "library"]
        mappings = process.memory_maps(grouped=True)
        for row in declared:
            if not any(same_windows_path(mapping.path, row["path"]) for mapping in mappings):
                raise PreflightError("lab_library_not_loaded")
        for mapping in mappings:
            path = Path(mapping.path)
            if (
                path.suffix.lower() == ".dll"
                and path.parent == Path(engine.path).parent
                and not any(same_windows_path(mapping.path, row["path"]) for row in declared)
            ):
                raise PreflightError("lab_unbound_engine_library")
    except (OSError, psutil.Error) as exc:
        raise PreflightError("lab_windows_identity_incomplete") from exc
    return args
