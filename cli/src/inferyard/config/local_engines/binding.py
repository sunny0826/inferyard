"""Persist a verified service identity into a new immutable binding output."""

from inferyard.config.loader import load_config, validate_endpoint
from inferyard.config.preparation_io import (
    NewDirectory,
    PreparationError,
    base_details,
    finish,
    new_path,
    sha256,
)
from inferyard.config.toml_writer import render
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import process_start_ticks
from inferyard.platforms.local_engines import bind_service, verify_bound_service


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
    verify_bound_service(config)
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
