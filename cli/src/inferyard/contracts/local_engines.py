"""Semantic rules for explicitly selected local-engine formal profiles."""

from pathlib import Path

from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES, LOCAL_ADAPTER, LOCAL_CONFIG
from inferyard.contracts.local_environment import is_absolute_path, validate_root_environment
from inferyard.errors import ContractError


def require(condition, field, reason):
    if not condition:
        raise ContractError(field, reason)


def validate_config(config):
    engine, model = config["engine"], config["model"]
    family = engine["family"]
    require(config.get("definition") == LOCAL_CONFIG, "config.definition", "unsupported definition")
    require(engine["adapter"] == LOCAL_ADAPTER, "config.engine.adapter", "profile mismatch")
    if family in DIRECTORY_FAMILIES:
        allowed_kinds = ("directory",)
    elif family == "polystrata":
        allowed_kinds = ("gguf", "directory")
    else:
        allowed_kinds = ("gguf",)
    require(model["kind"] in allowed_kinds, "config.model.kind", "engine/model kind mismatch")
    require(
        model["kind"] != "gguf" or Path(model["local_path"]).suffix.lower() == ".gguf",
        "config.model.local_path",
        "single GGUF required",
    )
    require(
        (family == "polystrata") == ("polystrata_config_path" in engine),
        "config.engine.polystrata_config_path",
        "polystrata_config_path is required for polystrata only",
    )
    require(
        (family == "polystrata") == ("polystrata_config_sha256" in engine),
        "config.engine.polystrata_config_sha256",
        "polystrata_config_sha256 is required for polystrata only",
    )
    if "polystrata_config_path" in engine:
        require(
            is_absolute_path(engine["polystrata_config_path"]),
            "config.engine.polystrata_config_path",
            "absolute path required",
        )
    require(
        family == "polystrata" or engine["backend"] == "metal",
        "config.engine.backend",
        "non-polystrata local profiles require metal backend",
    )
    require(
        "reasoning_effort" not in config["generation"] or family == "polystrata",
        "config.generation.reasoning_effort",
        "reasoning_effort is supported for polystrata only",
    )
    require(
        ("binding_path" in config["endpoint"]) == ("binding_sha256" in config["endpoint"]),
        "config.endpoint",
        "binding path and hash must occur together",
    )
    from inferyard.contracts.engine_tuning import validate_tuning

    validate_tuning(engine)
    environment = engine["environment"]
    validate_root_environment(family, environment, engine["working_directory"])
    require(
        config["conditions"]["model_loaded"] is True,
        "config.conditions.model_loaded",
        "formal binding requires a loaded local model",
    )
    require(
        config["conditions"]["ac_online"] is True,
        "config.conditions.ac_online",
        "formal local-engine runs require AC power",
    )
    require(
        config["generation"]["max_tokens"] <= config["conditions"]["context_size"],
        "config.generation.max_tokens",
        "exceeds total context size",
    )
