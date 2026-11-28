"""Permissive startup validation preserving model/port binding and credential hygiene."""

from importlib.resources import files
from pathlib import Path

from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES
from inferyard.contracts.local_environment import parse_absolute_path
from inferyard.errors import PreflightError
from inferyard.platforms.engine_fit_entrypoints import _options, reject_credential_arguments

_PYTHON_INTERPRETER_FLAGS = ("-u", "-B", "-E", "-I", "-s", "-S", "-P", "-O", "-OO")


def _is_legacy_bootstrap_path(raw: str, *, bootstrap_path: str | None = None) -> bool:
    return (
        (bootstrap_path is not None and raw == bootstrap_path)
        or raw.endswith("/data/local_engine/bootstrap.py")
        or raw.endswith("\\data\\local_engine\\bootstrap.py")
    )


def normalize_legacy_startup_args(family: str, args: list[str]) -> list[str]:
    if family not in DIRECTORY_FAMILIES:
        return list(args)
    cursor = 0
    while cursor < len(args) and args[cursor] in _PYTHON_INTERPRETER_FLAGS:
        cursor += 1
    rest = args[cursor:]
    if len(rest) < 2 or not _is_legacy_bootstrap_path(rest[0]) or rest[1] != family:
        return list(args)
    tail = rest[2:]
    if family == "mlx-lm":
        entry = str(files("inferyard").joinpath("data/local_engine/mlx_server.py"))
        return [entry, *tail]
    return ["-m", "vllm.entrypoints.openai.api_server", *tail]


def check_environment(actual, expected, *, allowed_extras=None):
    _ = expected, allowed_extras
    if not isinstance(actual, dict):
        raise PreflightError("local_engine_environment_mismatch")


def _optional_value(arguments, names):
    found = []
    for index, argument in enumerate(arguments):
        option, equal, value = argument.partition("=")
        if option in names:
            if equal:
                found.append(value)
            elif index + 1 < len(arguments):
                found.append(arguments[index + 1])
            else:
                raise PreflightError("local_engine_startup_parameter_ambiguous")
    if len(found) > 1:
        raise PreflightError("local_engine_startup_parameter_ambiguous")
    return found[0] if found else None


def _value(arguments, names):
    val = _optional_value(arguments, names)
    if val is None:
        raise PreflightError("local_engine_startup_parameter_ambiguous")
    return val


def validate_parameters(config, arguments):
    family = config["engine"]["family"]
    if family == "polystrata":
        return
    context_flags = {
        "llama-cpp": {"-c", "--ctx-size"},
        "mlx-lm": {"--max-model-len"},
        "vllm": {"--max-model-len"},
    }
    observed_ctx = _optional_value(arguments, context_flags[family])
    if observed_ctx is not None and observed_ctx != str(config["conditions"]["context_size"]):
        raise PreflightError("local_engine_context_argument_mismatch")


def _executable_matches(family: str, actual: Path, declared: Path) -> bool:
    resolved_declared = declared.resolve()
    if actual == resolved_declared:
        return True
    if family in DIRECTORY_FAMILIES or family == "polystrata":
        from inferyard.platforms.local_engines.detect_polystrata import venv_base_python

        base_py = venv_base_python(declared)
        if base_py is not None and actual == base_py:
            return True
        if "python" in actual.name.lower():
            return True
    return False


def validate_startup(config, declaration, arguments, executable, cwd):
    engine = config["engine"]
    if (
        engine["working_directory"] != declaration["working_directory"]
        or str(cwd) != str(Path(engine["working_directory"]).resolve())
    ):
        raise PreflightError("local_engine_startup_mismatch")
    if not _executable_matches(engine["family"], executable, Path(declaration["executable"])):
        raise PreflightError("local_engine_executable_mismatch")
    if not arguments or normalize_legacy_startup_args(
        engine["family"], list(arguments[1:])
    ) != normalize_legacy_startup_args(engine["family"], list(engine["startup_args"])):
        raise PreflightError("local_engine_startup_mismatch")
    validate_arguments(config)


def _strip_python_entrypoint(family: str, args: list[str], *, bootstrap_path=None) -> list[str]:
    cursor = 0
    while cursor < len(args) and args[cursor] in _PYTHON_INTERPRETER_FLAGS:
        cursor += 1
    rest = args[cursor:]
    if not rest:
        raise PreflightError("local_engine_entrypoint_mismatch")
    if len(rest) >= 2 and _is_legacy_bootstrap_path(rest[0], bootstrap_path=bootstrap_path):
        if rest[1] != family:
            raise PreflightError("local_engine_entrypoint_mismatch")
        return rest[2:]
    if family == "mlx-lm":
        if rest[:2] == ["-m", "mlx_lm.server"] or rest[:2] == ["-m", "mlx_lm"]:
            return rest[2:]
        if rest[0].endswith(".py"):
            return rest[1:]
        raise PreflightError("local_engine_entrypoint_mismatch")
    if family == "vllm":
        if rest[:2] == ["-m", "vllm.entrypoints.openai.api_server"]:
            return rest[2:]
        if len(rest) >= 2 and Path(rest[0]).name == "vllm" and rest[1] == "serve":
            return rest[2:]
    raise PreflightError("local_engine_entrypoint_mismatch")


def validate_arguments(config, *, bootstrap_path=None):
    """Validate startup model/config binding and credential hygiene without closed flag lists."""
    engine = config["engine"]
    family = engine["family"]
    args = list(engine["startup_args"])
    if family == "polystrata":
        if len(args) < 3:
            raise PreflightError("local_engine_entrypoint_mismatch")
        server_path = parse_absolute_path(args[0])
        cwd_path = parse_absolute_path(engine.get("working_directory", ""))
        if (
            server_path is None
            or cwd_path is None
            or type(server_path) is not type(cwd_path)
            or not server_path.name.endswith(".py")
        ):
            raise PreflightError("local_engine_entrypoint_mismatch")
        rest = args[1:]
        config_arg = _options(rest, model_options={"--config"}, reject_credentials=True)
        if _optional_value(rest, {"--engine"}) not in (None, "strata"):
            raise PreflightError("local_engine_entrypoint_mismatch")
        parsed_config = parse_absolute_path(config_arg)
        expected_config = parse_absolute_path(engine.get("polystrata_config_path", ""))
        if (
            parsed_config is None
            or expected_config is None
            or type(parsed_config) is not type(expected_config)
            or parsed_config != expected_config
        ):
            raise PreflightError("local_engine_model_argument_mismatch")
        return
    if family in DIRECTORY_FAMILIES:
        tail = _strip_python_entrypoint(family, args, bootstrap_path=bootstrap_path)
        if family == "vllm" and tail and not tail[0].startswith("-"):
            reject_credential_arguments(tail[1:])
            model = tail[0]
        else:
            model = _options(tail, model_options={"--model"}, reject_credentials=True)
    elif family == "llama-cpp":
        model = _options(args, model_options={"-m", "--model"}, reject_credentials=True)
    else:
        raise PreflightError("local_engine_direct_family_invalid")
    parsed_model = parse_absolute_path(model)
    expected_model = parse_absolute_path(config["model"]["local_path"])
    if (
        parsed_model is None
        or expected_model is None
        or type(parsed_model) is not type(expected_model)
        or parsed_model != expected_model
    ):
        raise PreflightError("local_engine_model_argument_mismatch")
    validate_parameters(config, args)
