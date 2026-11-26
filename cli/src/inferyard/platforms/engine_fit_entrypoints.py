"""Explicit startup forms for single-asset diagnostic services."""

import os
from pathlib import Path

from inferyard.contracts.local_environment import has_embedded_url_credentials
from inferyard.errors import PreflightError

_CREDENTIAL_FLAGS = frozenset(
    {"--api-key", "--api-key-file", "--password", "--token", "-hft", "--hf-token"}
)


def reject_credential_arguments(arguments) -> None:
    for raw_arg in arguments:
        option, _, value = raw_arg.partition("=")
        lowered = option.strip().lower()
        if (
            lowered in _CREDENTIAL_FLAGS
            or lowered.startswith("sk-")
            or value.strip().lower().startswith("sk-")
            or has_embedded_url_credentials(raw_arg)
        ):
            raise PreflightError("engine_credentials_forbidden")


def _options(arguments, _valued=None, _flags=None, model_options=(), *, reject_credentials=False):
    if reject_credentials:
        reject_credential_arguments(arguments)
    models = []
    cursor = 0
    while cursor < len(arguments):
        raw_arg = arguments[cursor]
        option, equal, value = raw_arg.partition("=")
        if option in model_options:
            if not equal:
                cursor += 1
                if cursor == len(arguments):
                    raise PreflightError("engine_fit_model_argument_ambiguous")
                value = arguments[cursor]
            if not value or value.startswith("-"):
                raise PreflightError("engine_fit_model_argument_ambiguous")
            models.append(value)
        elif option == "--max-model-len":
            if not equal:
                cursor += 1
                if cursor == len(arguments):
                    raise PreflightError("engine_fit_model_argument_ambiguous")
                value = arguments[cursor]
            try:
                valid_length = int(value) >= 1
            except ValueError:
                valid_length = False
            if not valid_length:
                raise PreflightError("engine_fit_model_argument_ambiguous")
        cursor += 1
    if len(models) != 1:
        raise PreflightError("engine_fit_model_argument_ambiguous")
    return models[0]


def _mlx_script(arguments, cwd):
    from inferyard.platforms.engine_fit import stable_file_digest

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
    digest, _ = stable_file_digest(script)
    model = _options(arguments[cursor + 1 :], model_options={"--model"})
    return model, {"entrypoint_sha256": digest}


def startup_binding(engine, arguments, executable, cwd):
    """Return the declared model, binding source and extra immutable fields."""
    from inferyard.platforms.engine_fit import _startup_model

    _ = executable
    if not arguments:
        raise PreflightError("engine_fit_arguments_unavailable")
    reject_credential_arguments(arguments[1:])
    extras = {}
    if engine == "llama-cpp":
        model = _options(arguments[1:], model_options={"-m", "--model"})
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
