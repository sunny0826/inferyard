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
from inferyard.config.toml_writer import render
from inferyard.platforms.identity import process_start_ticks, sanitized_arguments


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
    start = read_ticks(pid)
    command = read_argv(pid)
    if read_ticks(pid) != start:
        raise PreparationError("service_identity_changed")
    expected = config["engine"]["startup_args"]
    origin = urlsplit(endpoint)
    for i, value in enumerate(expected[:-1]):
        if value == "--port":
            expected[i + 1] = str(origin.port or (443 if origin.scheme == "https" else 80))
        elif value == "--host":
            expected[i + 1] = origin.hostname
    if sanitized_arguments(command[1:]) != expected:
        raise PreparationError("startup_arguments_mismatch")
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
