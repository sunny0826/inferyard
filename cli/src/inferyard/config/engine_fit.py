"""Frozen diagnostic plans; independent of the formal single-file model contract."""

import hashlib
import math
import re
from pathlib import PurePosixPath, PureWindowsPath

from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import atomic_bytes, json_bytes, read_json

LEGACY_ENGINES = ("vllm", "sglang")
ENGINES = (*LEGACY_ENGINES, "llama-cpp", "mlx-lm", "lmstudio", "ollama")
DEFAULT_CASES = [
    {"id": "arithmetic", "prompt": "What is 17 + 25? Reply with only the integer."},
    {"id": "json", "prompt": 'Return only valid JSON with key "status" and value "ok".'},
    {"id": "chinese", "prompt": "请用一句中文解释什么是本地推理。"},
]


def _require(condition, field="plan"):
    if not condition:
        raise ContractError("engine_fit." + field, "invalid diagnostic input")


def _keys(value, keys, field):
    _require(isinstance(value, dict) and set(value) == set(keys.split()), field)


def _integer(value, low, high):
    return type(value) is int and low <= value <= high


def _sha(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def _override_reason(value, field="temperature_stop_override_reason"):
    _require(isinstance(value, str) and bool(value.strip()), field)
    try:
        _require(len(value.encode("utf-8")) <= 1024, field)
    except UnicodeEncodeError as exc:
        raise ContractError("engine_fit." + field, "invalid diagnostic input") from exc


def digest(value):
    return hashlib.sha256(json_bytes(value)).hexdigest()


def validate_cases(cases):
    _require(isinstance(cases, list) and 1 <= len(cases) <= 1000, "cases")
    identifiers = []
    for case in cases:
        _keys(case, "id prompt", "case")
        _require(
            isinstance(case["id"], str)
            and re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}", case["id"]),
            "case.id",
        )
        _require(
            isinstance(case["prompt"], str) and 0 < len(case["prompt"].encode("utf-8")) <= 65536,
            "case.prompt",
        )
        identifiers.append(case["id"])
    _require(len(set(identifiers)) == len(identifiers), "case.id")


def validate_plan(plan):
    _keys(
        plan,
        "schema_version definition plan_id model host engines cases parameters "
        "request_count max_request_wall_seconds",
        "plan",
    )
    from inferyard.evidence.formats import require_core, require_version

    _require(
        _sha(plan["plan_id"])
        and plan["plan_id"] == digest({k: v for k, v in plan.items() if k != "plan_id"}),
        "plan_id",
    )
    require_core(plan, "engine-fit plan")
    require_version(
        plan, "definition", tuple(f"engine_fit_plan.v{i}" for i in range(1, 5)), "engine-fit plan"
    )
    override = plan["definition"] == "engine_fit_plan.v3"
    memory_override = plan["definition"] == "engine_fit_plan.v4"
    modern = plan["definition"] != "engine_fit_plan.v1"
    _require(
        _sha(plan["plan_id"])
        and plan["plan_id"] == digest({k: v for k, v in plan.items() if k != "plan_id"}),
        "plan_id",
    )
    engines = plan["engines"]
    _require(isinstance(engines, list) and 1 <= len(engines) <= len(ENGINES), "engines")
    supported = ENGINES if modern else LEGACY_ENGINES
    _require(all(isinstance(e, str) and e in supported for e in engines), "engines")
    _require(len(set(engines)) == len(engines), "engines")
    model = plan["model"]
    model_keys = "path sha256 files kind" if modern else "path sha256 files"
    if modern and model.get("kind") == "directory" and "definition" not in model:
        from inferyard.evidence.formats import UnsupportedFormat

        raise UnsupportedFormat("model-assets", "unversioned", ("model-assets.v2",))
    if "definition" in model:
        _require(
            modern
            and model.get("kind") == "directory"
            and model["definition"] == "model-assets.v2",
            "model.definition",
        )
        model_keys += " definition"
    _keys(model, model_keys, "model")
    if modern:
        _require(model["kind"] in ("directory", "gguf"), "model.kind")
        allowed = (
            ("llama-cpp", "lmstudio", "ollama")
            if model["kind"] == "gguf"
            else (*LEGACY_ENGINES, "mlx-lm")
        )
        _require(all(engine in allowed for engine in engines), "engine_asset_kind")
    _require(
        isinstance(model["path"], str)
        and (
            PurePosixPath(model["path"]).is_absolute()
            or PureWindowsPath(model["path"]).is_absolute()
        ),
        "model.path",
    )
    files = model["files"]
    _require(isinstance(files, list) and files, "model.files")
    names = []
    for entry in files:
        _keys(entry, "path sha256 size", "model.file")
        name = entry["path"]
        _require(
            isinstance(name, str)
            and name
            and "\\" not in name
            and not PurePosixPath(name).is_absolute()
            and all(p not in ("", ".", "..") for p in name.split("/")),
            "model.file.path",
        )
        _require(_sha(entry["sha256"]) and _integer(entry["size"], 0, 2**63 - 1), "model.file")
        names.append(name)
    _require(names == sorted(set(names)), "model.files")
    if modern and model["kind"] == "gguf":
        path_type = PurePosixPath if PurePosixPath(model["path"]).is_absolute() else PureWindowsPath
        _require(len(files) == 1 and names == [path_type(model["path"]).name], "model.file")
        _require(path_type(model["path"]).suffix.lower() == ".gguf", "model.file")
    _require(_sha(model["sha256"]) and model["sha256"] == digest(files), "model.sha256")
    _require(isinstance(plan["host"], dict) and _sha(plan["host"].get("sha256")), "host")
    validate_cases(plan["cases"])
    parameters = plan["parameters"]
    _keys(
        parameters,
        "max_tokens temperature repetitions request_timeout_seconds "
        "min_free_memory_bytes min_free_disk_bytes max_temperature_celsius"
        + (" temperature_stop_override_reason" if override or memory_override else "")
        + (" memory_stop_override_reason" if memory_override else ""),
        "parameters",
    )
    _require(_integer(parameters["max_tokens"], 1, 8192), "max_tokens")
    _require(type(parameters["temperature"]) is int and parameters["temperature"] == 0)
    _require(_integer(parameters["repetitions"], 1, 1000), "repetitions")
    timeout = parameters["request_timeout_seconds"]
    _require(
        type(timeout) in (float, int) and math.isfinite(timeout) and 0 < timeout <= 600,
        "request_timeout_seconds",
    )
    if memory_override:
        _override_reason(parameters["memory_stop_override_reason"], "memory_stop_override_reason")
        _require(parameters["min_free_memory_bytes"] is None, "memory_safety")
    else:
        _require(_integer(parameters["min_free_memory_bytes"], 64 * 2**20, 2**63 - 1), "memory")
    _require(_integer(parameters["min_free_disk_bytes"], 64 * 2**20, 2**63 - 1), "disk")
    if override or (memory_override and parameters["temperature_stop_override_reason"] is not None):
        _override_reason(parameters["temperature_stop_override_reason"])
        _require(parameters["max_temperature_celsius"] is None, "temperature_safety")
    else:
        _require(
            type(parameters["max_temperature_celsius"]) is int
            and parameters["max_temperature_celsius"] == 85,
            "temperature_safety",
        )
    count = len(plan["cases"]) * parameters["repetitions"]
    _require(
        _integer(plan["request_count"], 1, 1000) and plan["request_count"] == count, "request_count"
    )
    budget = plan["max_request_wall_seconds"]
    _require(
        type(budget) in (int, float) and math.isfinite(budget) and budget == count * timeout,
        "max_request_wall_seconds",
    )
    return plan


def load_plan(path):
    return validate_plan(read_json(path))


def request_rows(plan):
    return [
        {
            "request_id": f"r{index:06d}",
            "case_id": case["id"],
            "status": "not_executed",
            "reason": "not_started",
            "response": None,
        }
        for index, case in enumerate(plan["cases"] * plan["parameters"]["repetitions"], 1)
    ]


def prepare(request):
    from inferyard.config.engine_fit_assets import model_asset_manifest
    from inferyard.platforms.engine_fit import host_identity, model_manifest

    _require(not request.out.exists() and not request.out.is_symlink(), "out")
    # Validate cheap input before hashing potentially large model weights.
    cases = read_json(request.fit_prompts) if request.fit_prompts else DEFAULT_CASES
    validate_cases(cases)
    _require(_integer(request.fit_max_tokens, 1, 8192), "max_tokens")
    _require(
        _integer(request.fit_repetitions, 1, 1000) and len(cases) * request.fit_repetitions <= 1000,
        "repetitions",
    )
    _require(
        type(request.fit_timeout) in (int, float)
        and math.isfinite(request.fit_timeout)
        and 0 < request.fit_timeout <= 600,
        "request_timeout",
    )
    _require(_integer(request.fit_min_memory_mib, 64, 2**40), "min_free_memory_mib")
    override_reason = request.fit_temperature_stop_override_reason
    if override_reason is not None:
        _override_reason(override_reason)
    memory_reason = request.fit_memory_stop_override_reason
    if memory_reason is not None:
        _override_reason(memory_reason, "memory_stop_override_reason")
    engines = request.fit_engines or (
        list(LEGACY_ENGINES) if request.model_path.is_dir() else ["llama-cpp", "lmstudio"]
    )
    _require(len(set(engines)) == len(engines) and all(e in ENGINES for e in engines), "engines")
    model_path = request.model_path
    _require(not request.out.resolve().is_relative_to(model_path.resolve()), "out_inside_model")
    modern = (
        override_reason is not None
        or memory_reason is not None
        or not model_path.is_dir()
        or any(e not in LEGACY_ENGINES for e in engines)
    )
    plan = {
        "schema_version": 3,
        "definition": "engine_fit_plan.v4"
        if memory_reason is not None
        else "engine_fit_plan.v3"
        if override_reason is not None
        else "engine_fit_plan.v2"
        if modern
        else "engine_fit_plan.v1",
        "model": model_asset_manifest(model_path) if modern else model_manifest(model_path),
        "host": host_identity(),
        "engines": engines,
        "cases": cases,
        "parameters": {
            "max_tokens": request.fit_max_tokens,
            "temperature": 0,
            "repetitions": request.fit_repetitions,
            "request_timeout_seconds": request.fit_timeout,
            "min_free_memory_bytes": request.fit_min_memory_mib * 2**20,
            "min_free_disk_bytes": 256 * 2**20,
            "max_temperature_celsius": 85,
        },
        "request_count": len(cases) * request.fit_repetitions,
        "max_request_wall_seconds": len(cases) * request.fit_repetitions * request.fit_timeout,
    }
    if override_reason is not None:
        plan["parameters"].update(
            max_temperature_celsius=None, temperature_stop_override_reason=override_reason
        )
    if memory_reason is not None:
        plan["parameters"].update(
            min_free_memory_bytes=None,
            memory_stop_override_reason=memory_reason,
            temperature_stop_override_reason=override_reason,
        )
    plan["plan_id"] = digest(plan)
    validate_plan(plan)
    request.out.mkdir(parents=True, mode=0o700)
    atomic_bytes(request.out / "plan.json", json_bytes(plan))
    return plan
