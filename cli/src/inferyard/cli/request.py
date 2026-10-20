"""Validate CLI bindings and construct application requests before dispatch."""

import argparse
import math
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
    if args.command == "plan" and bool(values.get("preset")) != bool(values.get("config")):
        raise ArgumentError("--config requires --preset and vice versa")
    if args.command == "config create":
        local = values.get("installation") is not None
        if local:
            if values.get("model_path") is None or values.get("output_root") is None:
                raise ArgumentError("--installation requires --model and --results")
            if any(
                values.get(key) for key in ("benchmark_profile", "preflight", "model_source_record")
            ):
                raise ArgumentError(
                    "local installations do not accept --profile, --preflight or --model-source"
                )
            context = values.get("context_size")
            tokens = values.get("max_tokens")
            timeout = values.get("timeout_seconds")
            if context is not None and not 1 <= context <= 32768:
                raise ArgumentError("--context-size must be in 1..32768")
            if tokens is not None and not 1 <= tokens <= 8192:
                raise ArgumentError("--max-tokens must be in 1..8192")
            if timeout is not None and (not math.isfinite(timeout) or timeout <= 0):
                raise ArgumentError("--timeout-seconds must be positive and finite")
            if values.get("server_pid") is not None and values["server_pid"] <= 0:
                raise ArgumentError("--service-pid must be positive")
        else:
            if any(
                values.get(key) is not None
                for key in (
                    "model_path",
                    "served_model",
                    "max_tokens",
                    "timeout_seconds",
                    "server_pid",
                )
            ):
                raise ArgumentError("local model options require --installation")
            if bool(values.get("benchmark_profile")) != bool(values.get("context_size")):
                raise ArgumentError("--profile and --context-size must be provided together")
            if values.get("context_size") not in (None, 65536, 131072, 262144):
                raise ArgumentError("unsupported context-speed context size")
            if values.get("workspace") is None:
                if values.get("preflight") is None or values.get("output_root") is None:
                    raise ArgumentError(
                        "--preflight and --results are required without --workspace"
                    )
    if any(pid <= 0 for pid in values.get("protect_pids") or ()):
        raise ArgumentError("protected PID must be positive")
    from_run = values.get("from_run")
    community = args.command in (
        "init",
        "runtime prepare",
        "config assets",
        "config create",
        "config bind",
        "config describe",
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
    if values.get("service_binding_path") is not None and (
        not (experiment_execution or rerun_execution)
        or values.get("endpoint_url") is None
        or values.get("server_pid") is None
    ):
        raise ArgumentError("--service-binding requires an explicit rerun or experiment binding")
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
        community
        or args.command == "model acquire"
        or args.command == "engine acquire"
        or args.command == "engine check"
        or args.command.startswith("engine-fit ")
    )
    if args.command == "config create" and options.get("workspace") is not None:
        bundle = options["bundle_path"]
        if not bundle.is_absolute() and bundle.parent == Path(".") and not bundle.suffix:
            options["bundle_path"] = options["workspace"] / "bundles" / (bundle.name + ".json")
    for key, value in options.items():
        if isinstance(value, Path):
            options[key] = value.absolute() if preserve_links else value.resolve()
    for key in ("report_runs", "models_roots"):
        if options.get(key):
            options[key] = [
                path.absolute() if preserve_links else path.resolve() for path in options[key]
            ]
    return CommandRequest(args.command, config=config, **options)
