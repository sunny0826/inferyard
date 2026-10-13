"""Explicit startup forms for single-asset diagnostic services."""

import os
from pathlib import Path

from inferyard.platforms.identity import PreflightError

_LLAMA_VALUES = {
    "-m",
    "--model",
    "--host",
    "--port",
    "-a",
    "--alias",
    "--api-key",
    "-c",
    "--ctx-size",
    "-t",
    "--threads",
    "-tb",
    "--threads-batch",
    "-np",
    "--parallel",
    "-ngl",
    "--gpu-layers",
    "--n-gpu-layers",
    "-b",
    "--batch-size",
    "-ub",
    "--ubatch-size",
    "-fa",
    "--flash-attn",
    "-ctk",
    "--cache-type-k",
    "-ctv",
    "--cache-type-v",
    "--seed",
    "--temp",
    "--top-k",
    "--top-p",
    "--min-p",
    "--repeat-penalty",
    "--chat-template",
    "--reasoning-format",
    "--reasoning-budget",
    "--log-verbosity",
    "--timeout",
    "--threads-http",
    "--poll",
    "--poll-batch",
}
_LLAMA_FLAGS = {
    "--metrics",
    "--slots",
    "--no-webui",
    "--offline",
    "--jinja",
    "--no-jinja",
    "--no-mmap",
    "--mlock",
    "--no-warmup",
    "--no-context-shift",
    "--no-kv-offload",
    "--no-cont-batching",
    "--cont-batching",
    "--log-disable",
    "--log-colors",
    "--log-prefix",
    "--log-timestamps",
}
_MLX_VALUES = {
    "--model",
    "--host",
    "--port",
    "--served-model-name",
    "--api-key-env",
    "--max-model-len",
}


def _options(arguments, valued, flags, model_options):
    models, seen, cursor = [], set(), 0
    while cursor < len(arguments):
        option, equal, value = arguments[cursor].partition("=")
        if option not in valued | flags or option in seen:
            raise PreflightError("engine_fit_model_argument_ambiguous")
        seen.add(option)
        if option in flags:
            if equal:
                raise PreflightError("engine_fit_model_argument_ambiguous")
        else:
            if not equal:
                cursor += 1
                if cursor == len(arguments):
                    raise PreflightError("engine_fit_model_argument_ambiguous")
                value = arguments[cursor]
            if not value or (option in model_options and value.startswith("-")):
                raise PreflightError("engine_fit_model_argument_ambiguous")
            if option == "--max-model-len":
                try:
                    valid_length = 1 <= int(value) <= 32768
                except ValueError:
                    valid_length = False
                if not valid_length:
                    raise PreflightError("engine_fit_model_argument_ambiguous")
            if option in model_options:
                models.append(value)
        cursor += 1
    if len(models) != 1:
        raise PreflightError("engine_fit_model_argument_ambiguous")
    return models[0]


def _mlx_script(arguments, cwd):
    from inferyard.config.engine_fit_assets import mlx_server_sha256
    from inferyard.platforms.engine_fit import _file_hash

    if not arguments:
        raise PreflightError("engine_fit_startup_entrypoint_unverified")
    cursor = 1
    while cursor < len(arguments) and arguments[cursor] in (
        "-u",
        "-B",
        "-E",
        "-I",
        "-s",
        "-S",
        "-P",
        "-O",
        "-OO",
    ):
        cursor += 1
    if cursor == len(arguments) or arguments[cursor].startswith("-"):
        raise PreflightError("engine_fit_startup_entrypoint_unverified")
    script = Path(arguments[cursor])
    if not script.is_absolute():
        script = cwd / script
    digest, _ = _file_hash(script)
    if digest != mlx_server_sha256():
        raise PreflightError("engine_fit_mlx_entrypoint_changed")
    model = _options(arguments[cursor + 1 :], _MLX_VALUES, set(), {"--model"})
    return model, {"entrypoint_sha256": digest}


def startup_binding(engine, arguments, executable, cwd, *, windows=False):
    """Return the declared model, binding source and extra immutable fields."""
    from inferyard.platforms.engine_fit import _startup_model

    if not arguments:
        raise PreflightError("engine_fit_arguments_unavailable")
    extras = {}
    if engine == "llama-cpp":
        # The OS executable digest, frozen GGUF, argv and server protocol bind identity.
        # A filename supplies none of that authority, including on Windows.
        model = _options(arguments[1:], _LLAMA_VALUES, _LLAMA_FLAGS, {"-m", "--model"})
        source = "verified_startup_file_identity"
    else:
        if engine == "mlx-lm":
            model, extras = _mlx_script(arguments, cwd)
        else:
            model = _startup_model(engine, arguments)
        source = "verified_startup_directory_identity"
    path = Path(model)
    return (path if path.is_absolute() else cwd / path), source, extras


def validate_model(startup, frozen, source):
    path = Path(frozen).resolve(strict=True)
    correct_kind = (
        path.is_file() if source != "verified_startup_directory_identity" else path.is_dir()
    )
    if not correct_kind or not os.path.samefile(startup, path):
        raise PreflightError("engine_fit_model_argument_mismatch")
    return path


def lmstudio_executable(executable):
    if executable.name == "llmster":
        return
    if executable.name in ("LM Studio", "LM Studio Helper") and any(
        parent.name == "LM Studio.app" for parent in executable.parents
    ):
        return
    raise PreflightError("engine_fit_executable_entrypoint_unverified")


def reject_llama_environment(environment):
    """Require explicit arguments; never retain environment names or their values."""
    if not isinstance(environment, dict) or any(
        not isinstance(key, str)
        or not key
        or "=" in key
        or "\0" in key
        or not isinstance(value, str)
        or "\0" in value
        for key, value in environment.items()
    ):
        raise PreflightError("engine_fit_environment_unavailable")
    if any(key.startswith("LLAMA_ARG_") for key in environment):
        raise PreflightError("engine_fit_llama_environment_requires_explicit_args")


def linux_llama_environment(directory):
    try:
        with (directory / "environ").open("rb") as stream:
            raw = stream.read(1024 * 1024 + 1)
        if len(raw) > 1024 * 1024 or (raw and not raw.endswith(b"\0")):
            raise PreflightError("engine_fit_environment_unavailable")
        environment = {}
        for item in raw.removesuffix(b"\0").split(b"\0") if raw else []:
            key, equal, value = item.partition(b"=")
            name = os.fsdecode(key)
            if not equal or name in environment:
                raise PreflightError("engine_fit_environment_unavailable")
            environment[name] = os.fsdecode(value)
        reject_llama_environment(environment)
    except OSError as exc:
        raise PreflightError("engine_fit_environment_unavailable") from exc


def macos_llama_environment(pid, psutil):
    try:
        reject_llama_environment(psutil.Process(pid).environ())
    except (OSError, psutil.Error) as exc:
        raise PreflightError("engine_fit_environment_unavailable") from exc
