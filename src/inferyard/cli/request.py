"""Validate CLI bindings and construct application requests before dispatch."""

import argparse
from collections.abc import Callable
from pathlib import Path

from inferyard.application.types import CommandRequest
from inferyard.cli.arguments import ArgumentError
from inferyard.config.loader import LoadedConfig, load_config, validate_endpoint
from inferyard.contracts.validation import ContractError


def build_request(
    args: argparse.Namespace, *, config_loader: Callable[[Path], LoadedConfig] = load_config
) -> CommandRequest:
    values = vars(args)
    from_run = values.get("from_run")
    community = args.command in (
        "init",
        "runtime prepare",
        "config assets",
        "config create",
        "config bind",
    )
    if community:
        if values.get("port") is not None and not 1 <= values["port"] <= 65535:
            raise ArgumentError("invalid port")
        for key in ("model_repo", "model_revision"):
            if values.get(key) is not None and not values[key].strip():
                raise ArgumentError("model declaration must be nonempty")
        if args.command == "config bind":
            validate_endpoint(values["endpoint_url"])
            if values["server_pid"] <= 0:
                raise ArgumentError("PID must be positive")
    fit_execution = args.command == "engine-fit run"
    rerun_execution = args.command == "run" and from_run is not None
    experiment_execution = values.get("frozen_plan") is not None or args.command in (
        "resume",
        "prepare-length",
    )
    if values.get("frozen_plan") and values.get("output_root") is None and not fit_execution:
        raise ArgumentError("--plan requires --output-root")
    if (values.get("workload_id") or values.get("handoff_note")) and not experiment_execution:
        raise ArgumentError("workload and handoff options require experiment execution")
    if any(
        values.get(key) is not None
        for key in ("endpoint_url", "server_pid", "output_root", "api_key_env")
    ):
        if not community and not rerun_execution and not experiment_execution and not fit_execution:
            raise ArgumentError("endpoint/PID overrides require rerun or experiment execution")
    if rerun_execution:
        if values.get("endpoint_url") is None or values.get("server_pid") is None:
            raise ArgumentError("rerun requires --endpoint-url and --server-pid")
        validate_endpoint(values["endpoint_url"])
        if values["server_pid"] <= 0:
            raise ContractError("endpoint.server_pid", "must be positive")
    if experiment_execution:
        if (values.get("endpoint_url") is None) != (values.get("server_pid") is None):
            raise ArgumentError("service binding requires both endpoint and PID")
        if values.get("endpoint_url") is not None:
            validate_endpoint(values["endpoint_url"])
            if values["server_pid"] <= 0:
                raise ContractError("endpoint.server_pid", "must be positive")
    if bool(values.get("recovery_confirm")) != bool(values.get("recovery_note")):
        raise ArgumentError("recovery confirmation and note must be provided together")
    config = config_loader(values["config"]) if values.get("config") else None
    options = {
        key: values[key]
        for key in CommandRequest.__dataclass_fields__
        if key not in ("command", "config") and key in values
    }
    preserve_links = (
        community or args.command == "model acquire" or args.command.startswith("engine-fit ")
    )
    for key, value in options.items():
        if isinstance(value, Path):
            options[key] = value.absolute() if preserve_links else value.resolve()
    for key in ("report_runs", "models_roots"):
        if options.get(key):
            options[key] = [
                path.absolute() if preserve_links else path.resolve() for path in options[key]
            ]
    return CommandRequest(args.command, config=config, **options)
