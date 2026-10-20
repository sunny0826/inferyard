"""Bind the actual Darwin listener, Python implementation and local model."""

import hashlib
import os
from pathlib import Path

from inferyard.platforms import engine_fit_macos as native
from inferyard.platforms.identity import PreflightError, hash_file, service_origin
from inferyard.platforms.local_engines.assets import assets, bound_json
from inferyard.platforms.local_engines.installation import darwin
from inferyard.platforms.local_engines.startup import check_environment, validate_startup
from inferyard.platforms.local_engines.workers import mapped_closure, observe_workers
from inferyard.platforms.macos_identity import _listener_identity
from inferyard.platforms.macos_process import lsof_records, mapped_file

DEFINITION = "local-engine-binding.v1"


def validate_binding(binding, config):
    from inferyard.platforms.local_engines.binding_schema import validate

    return validate(binding, config)


def _direct(config, declaration, runtime_files, psutil):
    endpoint = config["endpoint"]
    pid = endpoint["server_pid"]
    state = native._state(pid, psutil)
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
    if native._state(pid, psutil) != state or psutil.Process(pid).environ() != environment:
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
        "model_binding": {
            "engine": config["engine"]["family"],
            "path": str(model),
            "device": info.st_dev,
            "inode": info.st_ino,
            "source": "verified_startup_directory_identity"
            if model.is_dir()
            else "verified_startup_file_identity",
        },
    }


def bind_service(config):
    darwin()
    runtime, runtime_files, _ = assets(config)
    declaration, engine, endpoint = runtime["installation"], config["engine"], config["endpoint"]
    worker_environment = declaration.get("worker_environment", engine["environment"])
    psutil = native.psutil_module()
    try:
        if engine["family"] == "lmstudio":
            # The existing observer verifies the loaded instance and mapped GGUF.
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
            state = native._state(endpoint["server_pid"], psutil)
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
        else:
            service = _direct(config, declaration, runtime_files, psutil)
        service["workers"] = observe_workers(
            endpoint["server_pid"], psutil, runtime_files, worker_environment
        )
        from inferyard.platforms.local_engines.installation import verify_installation

        verify_installation(runtime)
        final = native._state(endpoint["server_pid"], psutil)
        if (
            final[0] != service["start_ticks"]
            or hashlib.sha256(final[2]).hexdigest() != service["argv_sha256"]
            or hashlib.sha256(os.fsencode(final[4])).hexdigest() != service["cwd_sha256"]
            or observe_workers(endpoint["server_pid"], psutil, runtime_files, worker_environment)
            != service["workers"]
        ):
            raise PreflightError("local_engine_service_changed")
        check_environment(psutil.Process(endpoint["server_pid"]).environ(), engine["environment"])
        native.unique_listener(endpoint["server_pid"], service["address"], service["port"])
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


def verify_bound_service(config, *, bound_files=None):
    endpoint = config["endpoint"]
    binding, _ = bound_json(endpoint["binding_path"], endpoint["binding_sha256"])
    validate_binding(binding, config)
    if bound_files is not None and any(not item.unchanged() for item in bound_files):
        raise PreflightError("local_engine_bound_file_changed")
    current = bind_service(config)
    if current != binding:
        raise PreflightError("local_engine_bound_service_changed")
    return current["service"]
