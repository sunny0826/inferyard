"""Pure rules for declared root and subprocess environments, live and offline."""

from pathlib import PurePosixPath

from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES
from inferyard.contracts.validation import ContractError

_SGLANG_ROOT = {"SGLANG_RUN_ID", "PROMETHEUS_MULTIPROC_DIR"}
_VLLM_WORKER = {
    "VLLM_WORKER_MULTIPROC_METHOD",
    "MLX_MAX_OPS_PER_BUFFER",
    "VLLM_USE_V2_MODEL_RUNNER",
    "VLLM_OMP_NUM_THREADS_SET_BY_VLLM",
    "OMP_NUM_THREADS",
}
_SGLANG_WORKER = {
    "SGLANG_USE_CUDA_IPC_TRANSPORT",
    "SGLANG_ENABLE_TORCH_COMPILE",
    "SGLANG_DISABLE_OUTLINES_DISK_CACHE",
    "SGLANG_ENABLE_DETERMINISTIC_INFERENCE",
}


def _require(condition):
    if not condition:
        raise ContractError("installation.environment", "invalid declared process environment")


def _absolute(value):
    path = PurePosixPath(value)
    _require(path.is_absolute() and str(path) == value and ".." not in path.parts)
    return path


def validate_root_environment(family, environment, working_directory):
    _require(family == "sglang" or not environment.keys() & _SGLANG_ROOT)
    if "PROMETHEUS_MULTIPROC_DIR" in environment:
        path = _absolute(environment["PROMETHEUS_MULTIPROC_DIR"])
        cwd = _absolute(working_directory)
        _require(path != cwd and path.is_relative_to(cwd))


def validate_installation_environment(declaration):
    family, root = declaration["family"], declaration["environment"]
    validate_root_environment(family, root, declaration["working_directory"])
    worker = declaration.get("worker_environment", root)
    if "worker_environment" in declaration:
        _require(family in DIRECTORY_FAMILIES)
        _require(all(worker.get(key) == value for key, value in root.items()))
    _require(family == "vllm" or not worker.keys() & _VLLM_WORKER)
    _require(
        family == "sglang"
        or not worker.keys() & (_SGLANG_ROOT | _SGLANG_WORKER | {"SGLANG_USE_MLX"})
    )
    _require(family in DIRECTORY_FAMILIES or "TRANSFORMERS_NO_ADVISORY_WARNINGS" not in worker)
    if family == "sglang":
        _require(all(worker.get(key) == root.get(key) for key in _SGLANG_ROOT))
    if "VLLM_OMP_NUM_THREADS_SET_BY_VLLM" in worker:
        _require("OMP_NUM_THREADS" in worker)
    return worker
