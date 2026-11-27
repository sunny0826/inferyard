"""Freeze local installations, model metadata and declared formal conditions."""

from copy import deepcopy
from pathlib import Path

from inferyard.config.bundle import require_review
from inferyard.config.community_assets import model_template
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
from inferyard.contracts.engine_profiles import REASONING_EFFORT_LEVELS
from inferyard.contracts.schemas_config import CONFIG_DEFAULTS
from inferyard.contracts.validation import validate_document, json_bytes
from inferyard.errors import PreflightError
from inferyard.platforms.identity import environment_snapshot
from inferyard.platforms.local_engines import (
    freeze_installation,
    read_installation,
    verify_installation,
)
from inferyard.platforms.local_engines.startup import (
    _executable_matches,
    _optional_value,
    validate_arguments,
)

from .launch import startup
from .polystrata import (
    UPSTREAM_DEFAULT_CONTEXT,
    inspect_polystrata_payload,
    resolve_polystrata_model,
)

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


def _observe_managed_process(pid: int, system: str) -> tuple[list[str], Path, Path]:
    from inferyard.platforms import engine_fit_macos as native

    psutil = native.psutil_module()
    try:
        if system == "Darwin":
            _, arguments, _, executable, cwd = native.service_process_state(pid, psutil)
        else:
            from inferyard.platforms.local_engines.service import (
                _psutil_process_state,
                _verify_same_user,
            )

            proc = psutil.Process(pid)
            _verify_same_user(proc, psutil)
            _, arguments, _, executable, cwd = _psutil_process_state(pid, psutil)
    except (OSError, ValueError, IndexError, PreflightError, psutil.Error) as exc:
        raise PreparationError("local_engine_service_unavailable") from exc
    if not arguments:
        raise PreparationError("local_engine_service_unavailable")
    return list(arguments), executable, cwd


def _adopt_running_managed_service(
    pid: int,
    system: str,
    family: str,
    installation: dict,
    runtime: dict,
    expected_model: Path,
    context: int,
    served: str,
    fallback_tokenizer: Path | None,
):
    arguments, executable, observed_cwd = _observe_managed_process(pid, system)
    if not _executable_matches(family, executable, Path(installation["executable"])):
        raise PreparationError("local_engine_executable_mismatch")
    args = list(arguments[1:])
    cwd = str(observed_cwd)
    if cwd != runtime["installation"]["working_directory"]:
        runtime["installation"]["working_directory"] = cwd
        installation["working_directory"] = cwd
    if family == "polystrata":
        try:
            raw_cfg = _optional_value(args[1:], {"--config"})
        except PreflightError as exc:
            raise PreparationError(str(exc)) from exc
        if not raw_cfg:
            raise PreparationError("polystrata_config_required")
        try:
            ps_cfg_path = Path(raw_cfg).resolve(strict=True)
            raw_bytes = ps_cfg_path.read_bytes()
        except OSError as exc:
            raise PreparationError("polystrata_config_invalid") from exc
        payload = read_json(ps_cfg_path, "polystrata_config_invalid")
        if not isinstance(payload, dict):
            raise PreparationError("polystrata_config_invalid")
        bases: list[Path] = [Path(installation["working_directory"]), ps_cfg_path.parent]
        if installation.get("data_dir"):
            bases.append(Path(installation["data_dir"]))
        running_asset, existing_ctx, _, running_tok = inspect_polystrata_payload(
            payload, ps_cfg_path, tuple(bases)
        )
        if running_asset is None and (
            (ps_cfg_path.parent / "tokenizer_config.json").is_file()
            or (ps_cfg_path.parent / "chat_template.jinja").is_file()
        ):
            running_asset = ps_cfg_path.parent.resolve(strict=True)
        if running_asset is None or running_asset != expected_model:
            raise PreparationError("local_engine_model_argument_mismatch")
        adopted_ctx = existing_ctx if existing_ctx is not None else UPSTREAM_DEFAULT_CONTEXT
        raw_served = payload.get("model_name")
        adopted_served = (
            raw_served.strip()
            if isinstance(raw_served, str) and raw_served.strip()
            else served
        )
        tok = fallback_tokenizer if fallback_tokenizer is not None else running_tok
        return args, cwd, adopted_ctx, adopted_served, tok, ps_cfg_path, sha256(raw_bytes)

    ctx_flags = {
        "llama-cpp": {"-c", "--ctx-size"},
        "mlx-lm": {"--max-model-len"},
        "vllm": {"--max-model-len"},
        "sglang": {"--context-length"},
    }[family]
    try:
        obs_ctx = _optional_value(args, ctx_flags)
        obs_served = _optional_value(args, {"-a", "--alias", "--served-model-name"})
    except PreflightError as exc:
        raise PreparationError(str(exc)) from exc
    adopted_ctx = context
    if obs_ctx is not None:
        try:
            parsed_ctx = int(obs_ctx)
        except ValueError as exc:
            raise PreparationError("invalid_input") from exc
        if parsed_ctx <= 0:
            raise PreparationError("invalid_input")
        adopted_ctx = parsed_ctx
    adopted_served = (
        obs_served.strip() if obs_served is not None and obs_served.strip() else served
    )
    return args, cwd, adopted_ctx, adopted_served, fallback_tokenizer, None, None


def _model_kind(model: Path) -> str:
    try:
        if model.is_dir():
            return "directory"
        if model.is_file() and model.suffix.lower() == ".gguf":
            return "gguf"
    except OSError as exc:
        raise PreparationError("local_engine_model_unreadable") from exc
    raise PreparationError("local_engine_model_unreadable")


def create(request):
    if request.model_path is None or request.output_root is None:
        raise PreparationError("local_engine_explicit_model_and_results_required")
    installation = read_installation(request.installation)
    family = installation["family"]
    raw_effort = getattr(request, "reasoning_effort", None)
    if raw_effort is not None:
        if raw_effort not in REASONING_EFFORT_LEVELS:
            raise PreparationError("invalid_input")
        if family != "polystrata":
            raise PreparationError("reasoning_effort_unsupported")
    if family == "polystrata":
        system, _ = require_platform(("Darwin", "arm64"), ("Linux", "x64"), ("Windows", "x64"))
    else:
        system, _ = require_platform(("Darwin", "arm64"))
    inputs = (request.installation, request.model_path, request.bundle_path)
    out = new_path(request.out, inputs)
    model_input = request.model_path.resolve(strict=True)
    results = request.output_root.resolve()
    if results.is_relative_to(out) or (results.exists() and not results.is_dir()):
        raise PreparationError("invalid_input")
    runtime = freeze_installation(installation)
    verify_installation(runtime)

    port = 48857 if request.port is None else request.port
    if type(port) is not int or not 1 <= port <= 65535:
        raise PreparationError("invalid_input")

    polystrata_config_path = None
    polystrata_config_sha256 = None
    derived_polystrata_config = None
    polystrata_log_path: Path | None = None
    fallback_tokenizer = None
    if family == "polystrata":
        cand_parent = out.resolve().parent
        log_dir = (
            cand_parent.parent / "logs"
            if cand_parent.name == "candidates"
            else cand_parent / "logs"
        )
        polystrata_log_path = log_dir / "polystrata-engine.log"
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
            include_vision=False,
            managed_log_path=polystrata_log_path,
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

    reusing_managed = family != "lmstudio" and request.server_pid is not None
    if reusing_managed:
        (
            args,
            cwd,
            context,
            served,
            fallback_tokenizer,
            adopted_ps_path,
            adopted_ps_sha,
        ) = _adopt_running_managed_service(
            request.server_pid,
            system,
            family,
            installation,
            runtime,
            model,
            context,
            served,
            fallback_tokenizer,
        )
        if family == "polystrata":
            polystrata_config_path = adopted_ps_path
            polystrata_config_sha256 = adopted_ps_sha
            derived_polystrata_config = None

    model_kind = _model_kind(model)
    display_name, raw_template = template(model, fallback_tokenizer=fallback_tokenizer)
    bundle = read_json(request.bundle_path)
    require_review(bundle)
    environment = environment_snapshot()
    ac_state = environment.get("ac_online")
    if not reusing_managed:
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
    resolved_effort = (raw_effort or "none") if family == "polystrata" else None
    if request.max_tokens is not None:
        max_tokens = request.max_tokens
    elif resolved_effort not in (None, "none"):
        pelican = bundle["bundle_id"] == "zh-svg-pelican"
        target_tokens = (
            (4096 if resolved_effort == "low" else 8192)
            if pelican
            else (2048 if resolved_effort == "low" else 4096)
        )
        max_tokens = min(target_tokens, context)
    else:
        max_tokens = 4096 if bundle["bundle_id"] == "zh-svg-pelican" else 512
    generation_section = {
        "temperature": 0,
        "max_tokens": max_tokens,
    }
    if resolved_effort is not None:
        generation_section["reasoning_effort"] = resolved_effort

    config = deepcopy(CONFIG_DEFAULTS)
    config.update(
        schema_version=3,
        definition="local-engine-config.v1",
        device={"id": _DEVICE_IDS.get(system, "LAB-MACOS-001"), "client_position": "same_host_native"},
        bundle={"path": str(request.bundle_path.resolve()), "version": bundle["version"]},
        model={
            "display_name": display_name,
            "repo": request.model_repo or "local",
            "revision": request.model_revision or "local",
            "kind": model_kind,
            "local_path": str(model),
            "packing": "GGUF" if model_kind == "gguf" else "directory",
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
            "ac_online": bool(ac_state) if ac_state is not None else True,
            "profile": environment.get("profile") or "unknown",
            "governor": environment.get("governor") or "unknown",
            "epp": environment.get("epp") or "unknown",
            "allow_unknown_environment": True,
            "model_loaded": True,
            "background_load": "operator declaration; verified before execution",
        },
        safety={
            "interval_seconds": 1,
            "check_environment": False,
        },
    )
    if environment.get("macos_power_policy") is not None:
        config["conditions"]["macos_power_policy"] = environment["macos_power_policy"]
    if request.prompt_cache is not None:
        config["engine"]["tuning"] = {"prompt_cache": request.prompt_cache}
    config["output"].update(root=str(results), min_available_memory_bytes=1024**3)
    config["execution"]["timeout_seconds"] = request.timeout_seconds or 600
    config["execution"]["idle_wait_seconds"] = 30
    config["telemetry"]["service_rss_required"] = True
    validate_document("config", config)
    if reusing_managed:
        try:
            validate_arguments(config)
        except PreflightError as exc:
            raise PreparationError(str(exc)) from exc
    content = render(config)
    if template(model, fallback_tokenizer=fallback_tokenizer)[1] != raw_template:
        raise PreparationError("local_engine_model_assets_changed")
    verify_installation(runtime)
    output = NewDirectory(request.out, inputs)
    output.write("installation.json", json_bytes(runtime))
    output.write("chat-template.jinja", raw_template)
    if derived_polystrata_config is not None:
        if polystrata_log_path is not None:
            polystrata_log_path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
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
