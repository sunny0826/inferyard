"""Persist a verified service identity into a new immutable binding output."""

from pathlib import Path

from inferyard.config.loader import load_config
from inferyard.config.service_binding import arguments as process_arguments
from inferyard.config.toml_writer import render
from inferyard.contracts.endpoint import validate_endpoint
from inferyard.contracts.local_environment import parse_absolute_path
from inferyard.contracts.validation import json_bytes
from inferyard.errors import PreflightError
from inferyard.platforms.identity import hash_file, process_start_ticks
from inferyard.platforms.local_engines import bind_service
from inferyard.platforms.local_engines.assets import bound_json
from inferyard.platforms.local_engines.service import validate_binding
from inferyard.platforms.local_engines.startup import _optional_value
from inferyard.platforms.startup_arguments import replace_arguments
from inferyard.provisioning.preparation_io import (
    NewDirectory,
    PreparationError,
    base_details,
    finish,
    new_path,
    sha256,
)


def _reconcile_polystrata_sibling_config(config: dict, pid: int, expected_start: int) -> None:
    engine = config["engine"]
    if engine.get("family") != "polystrata":
        return
    try:
        live_argv = process_arguments(pid)
        if process_start_ticks(pid) != expected_start or len(live_argv) < 2:
            return
        live_cfg_raw = _optional_value(live_argv[2:], {"--config"})
    except (OSError, ValueError, PreflightError):
        return
    if not live_cfg_raw or live_cfg_raw == engine.get("polystrata_config_path"):
        return
    if parse_absolute_path(live_cfg_raw) is None:
        return
    live_cfg_path = Path(live_cfg_raw)
    try:
        if live_cfg_path.is_symlink() or not live_cfg_path.is_file():
            return
        hash_file(live_cfg_path, engine["polystrata_config_sha256"])
    except (OSError, PreflightError):
        return
    rewritten = replace_arguments(engine["startup_args"], {"--config": live_cfg_raw})
    if rewritten != list(live_argv[1:]):
        return
    engine["startup_args"] = rewritten
    engine["polystrata_config_path"] = live_cfg_raw


def prepare(request):
    validate_endpoint(request.endpoint_url)
    out = new_path(request.out, (request.candidate,))
    config = load_config(request.candidate).config.to_dict()
    if type(request.server_pid) is not int or request.server_pid <= 0:
        raise PreparationError("invalid_input")
    start = process_start_ticks(request.server_pid)
    config["endpoint"].update(
        url=request.endpoint_url,
        server_pid=request.server_pid,
        process_start_ticks=start,
    )
    config["endpoint"].pop("binding_path", None)
    config["endpoint"].pop("binding_sha256", None)
    _reconcile_polystrata_sibling_config(config, request.server_pid, start)
    binding = bind_service(config)
    config["endpoint"].update(
        binding_path=str(out / "binding.json"),
        binding_sha256=sha256(json_bytes(binding)),
    )
    content = render(config)
    output = NewDirectory(request.out, (request.candidate,))
    output.write("binding.json", json_bytes(binding))
    target = output.write("config.toml", content.encode())
    if load_config(target).config.to_dict() != config:
        raise PreparationError("serialization_mismatch", 4)
    endpoint = config["endpoint"]
    binding, _ = bound_json(endpoint["binding_path"], endpoint["binding_sha256"])
    validate_binding(binding, config)
    return finish(
        output,
        "preparation.json",
        "local-engine-binding-output.v1",
        {
            **base_details(output.path),
            "config": str(target),
            "candidate": str(request.candidate.resolve()),
            "pid": request.server_pid,
            "start_ticks": start,
            "endpoint": request.endpoint_url,
            "binding": str(out / "binding.json"),
        },
    )
