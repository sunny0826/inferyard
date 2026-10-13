"""Compatible v3 lab adapter fields and observation refusal rules."""

from pathlib import PureWindowsPath

from inferyard.adapters.lab_observation import (
    LabProtocolError,
    is_idle,
    parse_cancel,
    parse_lifecycle,
    parse_request,
)
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import json_bytes

LAB_ENGINES = {"kvmem": "gguf", "ninfer": "ninfer"}


def require(condition, field, reason):
    if not condition:
        raise ContractError(field, reason)


def validate_config(config):
    engine, model = config["engine"], config["model"]
    identifier = engine["adapter"]
    for key in engine.get("environment", {}):
        require(
            key in ("CUDA_VISIBLE_DEVICES", "CUDA_CACHE_DISABLE"),
            "config.engine.environment",
            "only CUDA device/cache overrides may be frozen here",
        )
    if identifier not in LAB_ENGINES:
        require(
            model.get("kind", "gguf") == "gguf", "config.model.kind", "legacy adapter requires GGUF"
        )
        return
    for key in ("kind", "bytes"):
        require(key in model, "config.model." + key, "required for a lab adapter")
    require(
        model["kind"] == LAB_ENGINES[identifier], "config.model.kind", "engine/asset kind mismatch"
    )
    suffix = ".gguf" if identifier == "kvmem" else ".ninfer"
    require(
        PureWindowsPath(model["local_path"]).suffix.lower() == suffix,
        "config.model.local_path",
        "engine/asset extension mismatch",
    )
    require(engine["backend"] in ("cpu", "cuda"), "config.engine.backend", "Windows CPU/CUDA only")
    for key in ("working_directory", "asset_manifest"):
        require(key in engine, "config.engine." + key, "required for a lab adapter")
    require(
        PureWindowsPath(engine["working_directory"]).is_absolute(),
        "config.engine.working_directory",
        "requires an absolute Windows path",
    )
    if identifier == "ninfer":
        for key in ("component_ledger_path", "component_ledger_sha256"):
            require(key in model, "config.model." + key, "required for a native container")
    require(
        config["generation"]["seed_support"] == "supported",
        "config.generation.seed_support",
        "seed support must be frozen",
    )
    require(
        config["generation"]["reasoning_mode"] == "off",
        "config.generation.reasoning_mode",
        "serial text baseline requires reasoning off",
    )
    require(
        config["conditions"]["cache_policy"] == "disabled",
        "config.conditions.cache_policy",
        "serial text baseline requires disabled cache",
    )
    require(
        not engine.get("slots_debug", False),
        "config.engine.slots_debug",
        "legacy slots diagnostics do not apply",
    )
    entries = engine["asset_manifest"]
    require(
        len(entries) <= 256 and sum(e["bytes"] for e in entries) <= 64 * 1024**3,
        "config.engine.asset_manifest",
        "manifest exceeds resource bounds",
    )
    names = [str(PureWindowsPath(e["path"])).casefold() for e in entries]
    require(len(set(names)) == len(names), "config.engine.asset_manifest", "duplicate asset path")
    require(
        all(PureWindowsPath(e["path"]).is_absolute() for e in entries),
        "config.engine.asset_manifest",
        "requires absolute Windows asset paths",
    )
    for path, role, digest, size in (
        (model["local_path"], "model", model["sha256"], model["bytes"]),
        (engine["binary_path"], "engine", engine["binary_sha256"], None),
        (model["template_path"], "template", model["template_sha256"], None),
        *(
            (
                [
                    (
                        model["component_ledger_path"],
                        "component_ledger",
                        model["component_ledger_sha256"],
                        None,
                    )
                ]
            )
            if identifier == "ninfer"
            else []
        ),
    ):
        matching = [e for e in entries if PureWindowsPath(e["path"]) == PureWindowsPath(path)]
        require(
            len(matching) == 1
            and matching[0]["role"] == role
            and matching[0]["sha256"] == digest
            and (size is None or matching[0]["bytes"] == size),
            "config.engine.asset_manifest",
            "asset binding missing or inconsistent",
        )
    for role in ("model", "engine", "template", "component_ledger"):
        require(
            sum(e["role"] == role for e in entries)
            == (1 if role != "component_ledger" or identifier == "ninfer" else 0),
            "config.engine.asset_manifest",
            "ambiguous asset role",
        )


def validate_event(kind, data):
    if kind == "lab_usage":
        for key, reason in data["usage_missing_reasons"].items():
            require(
                (data[key] is None) == (reason == "not_reported"),
                "event.lab_usage",
                "missing usage requires an explicit reason",
            )
            require(
                data[key] is None or reason is None,
                "event.lab_usage",
                "measured usage must not have a missing reason",
            )
        for part, total in (
            ("cached_tokens", "prompt_tokens"),
            ("reasoning_tokens", "completion_tokens"),
        ):
            require(
                data[part] is None or data[total] is None or data[part] <= data[total],
                "event.lab_usage",
                "usage detail exceeds total",
            )
        return
    lab = data.get("lab_snapshot")
    if lab is None:
        require(
            data["source"] != "/lab/v1/lifecycle",
            "event.idle_observed",
            "lab source requires snapshot",
        )
        return
    require(
        data["source"] == "/lab/v1/lifecycle"
        and set(lab) == {"lifecycle", "request", "cancellation"},
        "event.idle_observed",
        "invalid lab snapshot envelope",
    )
    try:
        instance = lab["lifecycle"]["server_instance_id"]
        life = parse_lifecycle(json_bytes(lab["lifecycle"]), expected_instance_id=instance)
        request = lab["request"]
        if request is not None:
            parse_request(
                json_bytes(request),
                expected_instance_id=instance,
                expected_request_id=request["request_id"],
            )
        cancel = lab["cancellation"]
        if cancel is not None:
            require(request is not None, "event.idle_observed", "cancel needs request observation")
            parsed = parse_cancel(
                json_bytes(cancel),
                expected_instance_id=instance,
                expected_request_id=request["request_id"],
            )
            require(
                "error" not in parsed, "event.idle_observed", "cancel error is not release proof"
            )
        idle = is_idle(life) and (request is None or request["phase"] == "released")
        require(
            (data["state"] == "idle") == idle,
            "event.idle_observed",
            "idle label differs from snapshot",
        )
    except (LabProtocolError, KeyError, TypeError, ValueError) as exc:
        if isinstance(exc, ContractError):
            raise
        raise ContractError("event.idle_observed", "invalid lab observation") from exc
