"""Freeze local installations, model bytes and declared formal conditions."""

from copy import deepcopy

from inferyard.config.bundle import require_review
from inferyard.config.community_assets import model_template
from inferyard.config.engine_fit_assets import model_asset_manifest
from inferyard.config.loader import load_config
from inferyard.config.preparation_io import (
    NewDirectory,
    PreparationError,
    base_details,
    finish,
    new_path,
    read_json,
    require_platform,
    sha256,
)
from inferyard.config.toml_writer import render
from inferyard.contracts.schemas_config import CONFIG_DEFAULTS
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import json_bytes
from inferyard.platforms.identity import environment_snapshot
from inferyard.platforms.local_engines import (
    freeze_installation,
    read_installation,
    verify_installation,
)

from .launch import startup


def template(model):
    if model.is_file():
        metadata, raw = model_template(model)
        return metadata.get("general.name") or model.stem, raw
    tokenizer = read_json(model / "tokenizer_config.json")
    value = tokenizer.get("chat_template")
    if not isinstance(value, str) or not value.strip():
        raise PreparationError("local_engine_chat_template_unavailable")
    return model.name, value.encode("utf-8")


def create(request):
    require_platform(("Darwin", "arm64"))
    if request.model_path is None or request.output_root is None:
        raise PreparationError("local_engine_explicit_model_and_results_required")
    inputs = (request.installation, request.model_path, request.bundle_path)
    out = new_path(request.out, inputs)
    if request.model_path.is_symlink():
        raise PreparationError("local_engine_model_symlink_rejected")
    model = request.model_path.resolve(strict=True)
    results = request.output_root.resolve()
    if results.is_relative_to(out) or (results.exists() and not results.is_dir()):
        raise PreparationError("invalid_input")
    installation = read_installation(request.installation)
    if installation["family"] != "lmstudio" and request.server_pid is not None:
        raise PreparationError("local_engine_service_pid_lmstudio_only")
    runtime = freeze_installation(installation)
    verify_installation(runtime)
    assets = model_asset_manifest(model)
    display_name, raw_template = template(model)
    bundle = read_json(request.bundle_path)
    require_review(bundle)
    environment = environment_snapshot()
    if environment.get("ac_online") is not True:
        raise PreparationError("ac_power_required")
    context = request.context_size or 8192
    served = request.served_model or "inferyard-local-model"
    args, cwd = startup(installation, model, served, context, request.port, request.server_pid)
    config = deepcopy(CONFIG_DEFAULTS)
    config.update(
        schema_version=3,
        definition="local-engine-config.v1",
        device={"id": "LAB-MACOS-001", "client_position": "same_host_native"},
        bundle={"path": str(request.bundle_path.resolve()), "version": bundle["version"]},
        model={
            "display_name": display_name,
            "repo": request.model_repo or "local",
            "revision": request.model_revision or assets["sha256"],
            "kind": assets["kind"],
            "local_path": str(model),
            "packing": "GGUF" if assets["kind"] == "gguf" else "directory",
            "assets_path": str(out / "model-assets.json"),
            "assets_sha256": sha256(json_bytes(assets)),
            "content_sha256": assets["sha256"],
            "template_path": str(out / "chat-template.jinja"),
            "template_sha256": sha256(raw_template),
        },
        engine={
            "adapter": "local_openai_v1",
            "family": installation["family"],
            "backend": "metal",
            "installation_path": str(out / "installation.json"),
            "installation_sha256": sha256(json_bytes(runtime)),
            "startup_args": args,
            "working_directory": cwd,
            "environment": installation["environment"],
            "served_model": served,
        },
        endpoint={
            "url": f"http://127.0.0.1:{request.port}",
            "server_pid": 1,
            "process_start_ticks": 1,
        },
        generation={
            "temperature": 0,
            "max_tokens": request.max_tokens
            or (4096 if bundle["bundle_id"] == "zh-svg-pelican" else 512),
        },
        conditions={
            "context_size": context,
            "cache_policy": "unknown",
            "ac_online": True,
            "profile": environment.get("profile") or "unknown",
            "governor": environment.get("governor") or "unknown",
            "epp": environment.get("epp") or "unknown",
            "allow_unknown_environment": True,
            "model_loaded": True,
            "background_load": "operator declaration; verified before execution",
        },
        safety={
            "interval_seconds": 1,
            "max_temperature_celsius": 85,
            "require_temperature": True,
            "check_environment": True,
        },
    )
    if environment.get("macos_power_policy") is not None:
        config["conditions"]["macos_power_policy"] = environment["macos_power_policy"]
    config["output"].update(root=str(results), min_available_memory_bytes=1024**3)
    config["execution"]["timeout_seconds"] = request.timeout_seconds or 600
    config["execution"]["idle_wait_seconds"] = 30
    config["telemetry"]["service_rss_required"] = True
    validate_document("config", config)
    content = render(config)
    if assets != model_asset_manifest(model) or template(model)[1] != raw_template:
        raise PreparationError("local_engine_model_assets_changed")
    verify_installation(runtime)
    output = NewDirectory(request.out, inputs)
    output.write("installation.json", json_bytes(runtime))
    output.write("model-assets.json", json_bytes(assets))
    output.write("chat-template.jinja", raw_template)
    candidate = output.write("candidate.toml", content.encode())
    if load_config(candidate).config.to_dict() != config:
        raise PreparationError("serialization_mismatch", 4)
    return finish(
        output,
        "preparation.json",
        "local-engine-candidate.v1",
        {
            **base_details(output.path),
            "candidate": str(candidate),
            "installation": str(out / "installation.json"),
            "family": installation["family"],
            "model": str(model),
            "bundle": str(request.bundle_path.resolve()),
        },
    )
