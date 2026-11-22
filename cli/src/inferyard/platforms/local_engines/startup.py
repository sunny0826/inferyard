"""Explicit formal startup syntax and frozen environment checks."""

from importlib.resources import files
from pathlib import Path

from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES
from inferyard.contracts.local_environment import parse_absolute_path
from inferyard.errors import PreflightError
from inferyard.platforms.engine_fit_entrypoints import (
    _LLAMA_FLAGS,
    _LLAMA_VALUES,
    _MLX_VALUES,
    _options,
)

_VALUES = {
    "--host",
    "--port",
    "--served-model-name",
    "--max-model-len",
    "--dtype",
    "--max-num-seqs",
    "--max-num-batched-tokens",
    "--gpu-memory-utilization",
    "--seed",
    "--tensor-parallel-size",
    "--pipeline-parallel-size",
    "--context-length",
    "--mem-fraction-static",
    "--max-running-requests",
    "--attention-backend",
    "--device",
    "--log-level",
    "--log-level-http",
}
_FLAGS = {
    "--disable-log-requests",
    "--enforce-eager",
    "--enable-metrics",
    "--disable-cuda-graph",
    "--disable-overlap-schedule",
    "--sleep-on-idle",
}
_PREFIXES = (
    "PYTHON",
    "DYLD_",
    "LD_",
    "LLAMA_ARG_",
    "VLLM_",
    "SGLANG_",
    "MLX_",
    "HF_",
    "TRANSFORMERS_",
    "TORCH_",
    "TOKENIZERS_",
    "UV_",
    "CONDA_",
    "SETUPTOOLS_",
    "OMP_",
    "PROMETHEUS_",
)


def check_environment(actual, expected, *, allowed_extras=None):
    if not isinstance(actual, dict) or any(actual.get(k) != v for k, v in expected.items()):
        raise PreflightError("local_engine_environment_mismatch")
    extras = allowed_extras or ()
    if any(
        key.startswith(_PREFIXES) and key not in expected and key not in extras for key in actual
    ):
        raise PreflightError("local_engine_environment_override")


def _value(arguments, names):
    found = []
    for index, argument in enumerate(arguments):
        option, equal, value = argument.partition("=")
        if option in names:
            found.append(value if equal else arguments[index + 1])
    if len(found) != 1:
        raise PreflightError("local_engine_startup_parameter_ambiguous")
    return found[0]


_POLYSTRATA_VALUES = {"--engine", "--config", "--host", "--port"}


def validate_parameters(config, arguments):
    family = config["engine"]["family"]
    if family == "polystrata":
        return
    context_flags = {
        "llama-cpp": {"-c", "--ctx-size"},
        "mlx-lm": {"--max-model-len"},
        "vllm": {"--max-model-len"},
        "sglang": {"--context-length"},
    }
    if _value(arguments, context_flags[family]) != str(config["conditions"]["context_size"]):
        raise PreflightError("local_engine_context_argument_mismatch")
    serial = {
        "llama-cpp": {"-np", "--parallel"},
        "vllm": {"--max-num-seqs"},
        "sglang": {"--max-running-requests"},
    }
    if family in serial and _value(arguments, serial[family]) != "1":
        raise PreflightError("local_engine_serial_argument_required")
    if family == "llama-cpp" and not {"--no-context-shift", "--metrics"} <= set(arguments):
        raise PreflightError("local_engine_llama_lifecycle_flags_required")
    if family == "sglang" and "--enable-metrics" not in arguments:
        raise PreflightError("local_engine_metrics_required")


def validate_startup(config, declaration, arguments, executable, cwd):
    engine = config["engine"]
    if (
        engine["working_directory"] != declaration["working_directory"]
        or engine["environment"] != declaration["environment"]
        or str(cwd) != str(Path(engine["working_directory"]).resolve())
    ):
        raise PreflightError("local_engine_startup_mismatch")
    allowed_executables = {Path(declaration["executable"]).resolve()}
    if engine["family"] == "polystrata":
        from inferyard.platforms.local_engines.detect_polystrata import venv_base_python

        base_py = venv_base_python(Path(declaration["executable"]))
        if base_py is not None:
            allowed_executables.add(base_py)
    if executable not in allowed_executables:
        raise PreflightError("local_engine_executable_mismatch")
    if arguments != [declaration["executable"], *engine["startup_args"]]:
        raise PreflightError("local_engine_startup_mismatch")
    validate_arguments(config)


def validate_arguments(config, *, bootstrap_path=None):
    """Validate frozen syntax without filesystem access, including offline reads."""
    engine = config["engine"]
    family = engine["family"]
    args = engine["startup_args"]
    if family == "polystrata":
        if len(args) < 3:
            raise PreflightError("local_engine_entrypoint_mismatch")
        server_path = parse_absolute_path(args[0])
        cwd_path = parse_absolute_path(engine.get("working_directory", ""))
        if (
            server_path is None
            or cwd_path is None
            or type(server_path) is not type(cwd_path)
            or server_path != cwd_path / "serve" / "server.py"
        ):
            raise PreflightError("local_engine_entrypoint_mismatch")
        rest = args[1:]
        config_arg = _options(rest, _POLYSTRATA_VALUES, set(), {"--config"})
        if _value(rest, {"--engine"}) != "strata":
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
        if args[:3] != ["-I", "-S", "-B"]:
            raise PreflightError("local_engine_python_isolation_required")
        script = bootstrap_path or str(
            files("inferyard").joinpath("data/local_engine/bootstrap.py")
        )
        if args[3:5] != [script, family]:
            raise PreflightError("local_engine_entrypoint_mismatch")
        if family == "mlx-lm":
            model = _options(args[5:], _MLX_VALUES, set(), {"--model"})
        else:
            option = "--model" if family == "vllm" else "--model-path"
            model = _options(args[5:], _VALUES | {option}, _FLAGS, {option})
    elif family == "llama-cpp":
        model = _options(args, _LLAMA_VALUES - {"--api-key"}, _LLAMA_FLAGS, {"-m", "--model"})
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
