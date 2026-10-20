"""Semantic rules for explicitly selected macOS native formal profiles."""

from pathlib import Path

from inferyard.contracts.engine_profiles import DIRECTORY_FAMILIES, LOCAL_ADAPTER, LOCAL_CONFIG
from inferyard.contracts.local_environment import validate_root_environment
from inferyard.contracts.validation import ContractError


def require(condition, field, reason):
    if not condition:
        raise ContractError(field, reason)


def validate_config(config):
    engine, model = config["engine"], config["model"]
    require(config.get("definition") == LOCAL_CONFIG, "config.definition", "unsupported definition")
    require(engine["adapter"] == LOCAL_ADAPTER, "config.engine.adapter", "profile mismatch")
    expected = "directory" if engine["family"] in DIRECTORY_FAMILIES else "gguf"
    require(model["kind"] == expected, "config.model.kind", "engine/model kind mismatch")
    require(
        model["kind"] != "gguf" or Path(model["local_path"]).suffix.lower() == ".gguf",
        "config.model.local_path",
        "single GGUF required",
    )
    require(
        ("binding_path" in config["endpoint"]) == ("binding_sha256" in config["endpoint"]),
        "config.endpoint",
        "binding path and hash must occur together",
    )
    environment = engine["environment"]
    validate_root_environment(engine["family"], environment, engine["working_directory"])
    require(
        environment.get("SGLANG_USE_MLX") == ("1" if engine["family"] == "sglang" else None),
        "config.engine.environment",
        "SGLang MLX selection must be explicit and engine-specific",
    )
    if engine["family"] in DIRECTORY_FAMILIES:
        require(
            all(
                environment.get(key) == "1"
                for key in (
                    "PYTHONDONTWRITEBYTECODE",
                    "HF_HUB_OFFLINE",
                    "TRANSFORMERS_OFFLINE",
                )
            ),
            "config.engine.environment",
            "Python profiles require frozen bytecode and offline loading",
        )
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
