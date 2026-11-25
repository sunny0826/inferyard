"""Freeze local installations, model bytes and declared formal conditions."""

from copy import deepcopy
from pathlib import Path

from inferyard.config.bundle import require_review
from inferyard.config.community_assets import model_template
from inferyard.platforms.local_engines.model_snapshot import snapshot as model_snapshot
from inferyard.config.loader import load_config
from inferyard.provisioning.preparation_io import (
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
from inferyard.contracts.validation import validate_document, json_bytes
from inferyard.platforms.identity import environment_snapshot
from inferyard.platforms.local_engines import (
    freeze_installation,
    read_installation,
    verify_installation,
)

from .launch import startup
from .polystrata import resolve_polystrata_model

_DEVICE_IDS = {
    "Darwin": "LAB-MACOS-001",
    "Linux": "LAB-LINUX-001",
    "Windows": "LAB-WINDOWS-001",
}


def _read_dir_template(directory: Path) -> bytes | None:
    cfg = directory / "tokenizer_config.json"
    if cfg.is_file():
        tokenizer = read_json(cfg)
        value = tokenizer.get("chat_template")
        if isinstance(value, str) and value.strip():
            return value.encode("utf-8")
    jinja = directory / "chat_template.jinja"
    if jinja.is_file():
        raw = jinja.read_bytes()
        if raw.strip():
            return raw
    return None


def template(model, *, fallback_tokenizer=None):
    if model.is_file():
        metadata, raw = model_template(model)
        return metadata.get("general.name") or model.stem, raw
    search_dirs: list[Path] = [model, model / "tokenizer"]
    if fallback_tokenizer is not None:
        if fallback_tokenizer.is_file():
            if fallback_tokenizer.suffix == ".json":
                tokenizer = read_json(fallback_tokenizer)
                value = tokenizer.get("chat_template")
                if isinstance(value, str) and value.strip():
                    return model.name, value.encode("utf-8")
            else:
                raw = fallback_tokenizer.read_bytes()
                if raw.strip():
                    return model.name, raw
        else:
            search_dirs.append(fallback_tokenizer)
    for directory in search_dirs:
        if directory.is_dir():
            found = _read_dir_template(directory)
            if found is not None:
                return model.name, found
    raise PreparationError("local_engine_chat_template_unavailable")


def create(request):
    if request.model_path is None or request.output_root is None:
        raise PreparationError("local_engine_explicit_model_and_results_required")
    installation = read_installation(request.installation)
    family = installation["family"]
    if family == "polystrata":
        system, _ = require_platform(("Darwin", "arm64"), ("Linux", "x64"), ("Windows", "x64"))
    else:
        system, _ = require_platform(("Darwin", "arm64"))
    inputs = (request.installation, request.model_path, request.bundle_path)
    out = new_path(request.out, inputs)
    if request.model_path.is_symlink():
        raise PreparationError("local_engine_model_symlink_rejected")
    model_input = request.model_path.resolve(strict=True)
    results = request.output_root.resolve()
    if results.is_relative_to(out) or (results.exists() and not results.is_dir()):
        raise PreparationError("invalid_input")
    if family != "lmstudio" and request.server_pid is not None:
        raise PreparationError("local_engine_service_pid_lmstudio_only")
    runtime = freeze_installation(installation)
    verify_installation(runtime)

    port = 48857 if request.port is None else request.port
    if type(port) is not int or not 1 <= port <= 65535:
        raise PreparationError("invalid_input")

    polystrata_config_path = None
    polystrata_config_sha256 = None
    derived_polystrata_config = None
    fallback_tokenizer = None
    if family == "polystrata":
        (
            model,
            polystrata_config_path,
            derived_polystrata_config,
            context,
            fallback_tokenizer,
            served,
            backend,
        ) = resolve_polystrata_model(
            installation,
            model_input,
            out,
            request.context_size,
            system=system,
            served_model=request.served_model,
            port=port,
        )
        if derived_polystrata_config is not None:
            polystrata_config_sha256 = sha256(json_bytes(derived_polystrata_config))
        else:
            polystrata_config_sha256 = sha256(polystrata_config_path.read_bytes())
    else:
        model = model_input
        context = request.context_size or 8192
        backend = "metal"
        served = request.served_model or "inferyard-local-model"

    assets, _ = model_snapshot(model)
    display_name, raw_template = template(model, fallback_tokenizer=fallback_tokenizer)
    bundle = read_json(request.bundle_path)
    require_review(bundle)
    environment = environment_snapshot()
    if system == "Darwin":
        if environment.get("ac_online") is not True:
            raise PreparationError("ac_power_required")
    elif environment.get("ac_online") is False:
        raise PreparationError("ac_power_required")
    args, cwd = startup(
        installation,
        model,
        served,
        context,
        port,
        request.server_pid,
        polystrata_config=polystrata_config_path,
    )
    engine_section = {
        "adapter": "local_openai_v1",
        "family": family,
        "backend": backend,
        "installation_path": str(out / "installation.json"),
        "installation_sha256": sha256(json_bytes(runtime)),
        "startup_args": args,
        "working_directory": cwd,
        "environment": installation["environment"],
        "served_model": served,
    }
    if polystrata_config_path is not None:
        engine_section["polystrata_config_path"] = str(polystrata_config_path)
        engine_section["polystrata_config_sha256"] = polystrata_config_sha256
    generation_section = {
        "temperature": 0,
        "max_tokens": request.max_tokens
        or (4096 if bundle["bundle_id"] == "zh-svg-pelican" else 512),
    }
    if family == "polystrata":
        generation_section["reasoning_effort"] = "none"

    config = deepcopy(CONFIG_DEFAULTS)
    config.update(
        schema_version=3,
        definition="local-engine-config.v1",
        device={"id": _DEVICE_IDS.get(system, "LAB-MACOS-001"), "client_position": "same_host_native"},
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
        engine=engine_section,
        endpoint={
            "url": f"http://127.0.0.1:{port}",
            "server_pid": 1,
            "process_start_ticks": 1,
        },
        generation=generation_section,
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
            "max_temperature_celsius": 115 if system == "Darwin" else 85,
            "require_temperature": system == "Darwin",
            "check_environment": True,
        },
    )
    if environment.get("macos_power_policy") is not None:
        config["conditions"]["macos_power_policy"] = environment["macos_power_policy"]
    if request.prompt_cache is not None:
        config["engine"]["tuning"] = {"prompt_cache": request.prompt_cache}
    if request.temperature_stop_override_reason is not None:
        config["safety"]["temperature_stop_override_reason"] = (
            request.temperature_stop_override_reason
        )
    config["output"].update(root=str(results), min_available_memory_bytes=1024**3)
    config["execution"]["timeout_seconds"] = request.timeout_seconds or 600
    config["execution"]["idle_wait_seconds"] = 30
    config["telemetry"]["service_rss_required"] = True
    validate_document("config", config)
    content = render(config)
    if (
        assets != model_snapshot(model)[0]
        or template(model, fallback_tokenizer=fallback_tokenizer)[1] != raw_template
    ):
        raise PreparationError("local_engine_model_assets_changed")
    verify_installation(runtime)
    output = NewDirectory(request.out, inputs)
    output.write("installation.json", json_bytes(runtime))
    output.write("model-assets.json", json_bytes(assets))
    output.write("chat-template.jinja", raw_template)
    if derived_polystrata_config is not None:
        output.write("strata-candidate.json", json_bytes(derived_polystrata_config))
    candidate = output.write("candidate.toml", content.encode())
    if load_config(candidate).config.to_dict() != config:
        raise PreparationError("serialization_mismatch", 4)
    details = {
        **base_details(output.path),
        "candidate": str(candidate),
        "installation": str(out / "installation.json"),
        "family": family,
        "model": str(model),
        "bundle": str(request.bundle_path.resolve()),
    }
    if polystrata_config_path is not None:
        details["polystrata_config"] = str(polystrata_config_path)
    return finish(
        output,
        "preparation.json",
        "local-engine-candidate.v1",
        details,
    )
