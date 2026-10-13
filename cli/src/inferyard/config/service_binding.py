"""Bind an existing service's observed identity; no service request or lifecycle operation."""

import os
import platform
from pathlib import Path
from urllib.parse import urlsplit

from inferyard.config.loader import load_config, validate_endpoint
from inferyard.config.preparation_io import (
    NewDirectory,
    PreparationError,
    base_details,
    finish,
    new_path,
    require_platform,
)
from inferyard.config.startup_arguments import replace_arguments
from inferyard.config.toml_writer import render
from inferyard.platforms.identity import PreflightError, process_start_ticks, sanitized_arguments


def arguments(pid):
    if os.name == "nt":
        from inferyard.platforms.windows_identity import process_arguments

        return process_arguments(pid)
    if platform.system() == "Darwin":
        from inferyard.platforms.macos_identity import process_arguments

        return process_arguments(pid)
    return (Path("/proc") / str(pid) / "cmdline").read_bytes().decode().rstrip("\0").split("\0")


def bind(candidate, pid, endpoint, *, ticks=None, argv=None):
    validate_endpoint(endpoint)
    if type(pid) is not int or pid <= 0:
        raise PreparationError("invalid_input")
    config = load_config(candidate).config.to_dict()
    read_ticks, read_argv = ticks or process_start_ticks, argv or arguments
    try:
        start = read_ticks(pid)
        command = read_argv(pid)
        current_start = read_ticks(pid)
    except PreflightError as exc:
        if str(exc) in ("service_process_unavailable", "service_identity_unreadable"):
            raise PreparationError(str(exc)) from None
        raise
    if current_start != start:
        raise PreparationError("service_identity_changed")
    origin = urlsplit(endpoint)
    expected = replace_arguments(
        config["engine"]["startup_args"],
        {
            "--host": origin.hostname,
            "--port": str(origin.port or (443 if origin.scheme == "https" else 80)),
        },
    )
    if sanitized_arguments(command[1:]) != expected:
        raise PreparationError("startup_arguments_mismatch")
    config["engine"]["startup_args"] = expected
    config["endpoint"].update(url=endpoint, server_pid=pid, process_start_ticks=start)
    return config, start


def prepare(request):
    require_platform(("Linux", "x64"), ("Windows", "x64"), ("Darwin", "arm64"))
    new_path(request.out, (request.candidate,))
    config, start = bind(request.candidate, request.server_pid, request.endpoint_url)
    try:
        content = render(
            config, comment="Bound to an external service; run performs current probes."
        )
    except ValueError:
        raise PreparationError("serialization_mismatch", 4) from None
    output = NewDirectory(request.out, (request.candidate,))
    out = output.write("config.toml", content.encode())
    if load_config(out).config.to_dict() != config:
        raise PreparationError("serialization_mismatch", 4)
    details = {
        **base_details(output.path),
        "config": str(out),
        "candidate": str(request.candidate.resolve()),
        "pid": request.server_pid,
        "start_ticks": start,
        "endpoint": request.endpoint_url,
        "preparation": str(output.path / "preparation.json"),
    }
    return finish(output, "preparation.json", "config_binding.v1", details)
