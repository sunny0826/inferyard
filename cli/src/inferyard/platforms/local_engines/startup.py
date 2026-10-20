"""Explicit formal startup syntax and frozen environment checks."""

from importlib.resources import files
from pathlib import Path, PurePosixPath

from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES
from inferyard.platforms.engine_fit_entrypoints import (
    _LLAMA_FLAGS,
    _LLAMA_VALUES,
    _MLX_VALUES,
    _options,
)
from inferyard.platforms.identity import PreflightError

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


def check_environment(actual, expected):
    if not isinstance(actual, dict) or any(actual.get(k) != v for k, v in expected.items()):
        raise PreflightError("local_engine_environment_mismatch")
    if any(key.startswith(_PREFIXES) and key not in expected for key in actual):
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


def validate_parameters(config, arguments):
    family = config["engine"]["family"]
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
        or arguments != [declaration["executable"], *engine["startup_args"]]
    ):
        raise PreflightError("local_engine_startup_mismatch")
    if executable != Path(declaration["executable"]).resolve():
        raise PreflightError("local_engine_executable_mismatch")
    validate_arguments(config)


def validate_arguments(config, *, bootstrap_path=None):
    """Validate frozen syntax without filesystem access, including offline reads."""
    engine = config["engine"]
    family = engine["family"]
    args = engine["startup_args"]
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
    if not PurePosixPath(model).is_absolute() or PurePosixPath(model) != PurePosixPath(
        config["model"]["local_path"]
    ):
        raise PreflightError("local_engine_model_argument_mismatch")
    validate_parameters(config, args)
