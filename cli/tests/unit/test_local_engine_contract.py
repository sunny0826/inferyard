"""Formal profiles retain asset meanings and reject undeclared capabilities."""

from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.config.loader import load_config, validate_runtime_config
from inferyard.config.single_plan import compile_single_plan
from inferyard.config.toml_writer import render
from inferyard.contracts.validation import ContractError


@pytest.fixture
def local_config(config_path):
    config = load_config(config_path).config.to_dict()
    config["definition"] = "local-engine-config.v1"
    config["model"] = {
        "display_name": "Local model",
        "repo": "local",
        "revision": "frozen",
        "kind": "directory",
        "local_path": "model",
        "packing": "4bit",
        "assets_path": "model-assets.json",
        "assets_sha256": "1" * 64,
        "content_sha256": "2" * 64,
        "template_path": "template.jinja",
        "template_sha256": "3" * 64,
    }
    config["engine"] = {
        "adapter": "local_openai_v1",
        "family": "mlx-lm",
        "backend": "metal",
        "installation_path": "runtime.json",
        "installation_sha256": "4" * 64,
        "startup_args": ["-I", "-B", "/installed/mlx_server.py", "--model", "/model"],
        "working_directory": "/runtime",
        "environment": {
            "PYTHONDONTWRITEBYTECODE": "1",
            "HF_HUB_OFFLINE": "1",
            "TRANSFORMERS_OFFLINE": "1",
        },
        "served_model": "formal-local",
    }
    config["generation"] = {"temperature": 0, "max_tokens": 512}
    for field in ("threads", "threads_batch", "slots"):
        config["conditions"].pop(field)
    config["conditions"].update(cache_policy="unknown", ac_online=True, model_loaded=True)
    config["safety"] = {
        "interval_seconds": 0.5,
        "max_temperature_celsius": 85,
        "require_temperature": True,
        "check_environment": True,
    }
    return config


@pytest.mark.parametrize("family", ["llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio"])
def test_explicit_formal_profile_accepts_only_its_asset_kind(local_config, family):
    local_config["engine"]["family"] = family
    if family in ("llama-cpp", "lmstudio"):
        local_config["model"].update(kind="gguf", local_path="model.gguf")
        local_config["engine"]["environment"] = {}
    if family == "sglang":
        local_config["engine"]["environment"]["SGLANG_USE_MLX"] = "1"
    validate_runtime_config(local_config)
    local_config["model"]["kind"] = (
        "gguf" if local_config["model"]["kind"] == "directory" else "directory"
    )
    with pytest.raises(ContractError, match="engine/model kind mismatch"):
        validate_runtime_config(local_config)


@pytest.mark.parametrize(
    "section,key,value",
    [
        ("model", "sha256", "5" * 64),
        ("generation", "top_p", 0.8),
        ("generation", "temperature", 0.7),
        ("generation", "max_tokens", True),
        ("engine", "backend", "cuda"),
        ("conditions", "cache_policy", "disabled"),
        ("conditions", "threads", 1),
        ("safety", "require_temperature", False),
        ("safety", "max_temperature_celsius", 86),
    ],
)
def test_unsupported_parameters_and_false_identity_are_rejected(local_config, section, key, value):
    local_config[section][key] = value
    with pytest.raises(ContractError):
        validate_runtime_config(local_config)


def test_python_environment_and_binding_pairs_are_required(local_config):
    altered = deepcopy(local_config)
    altered["engine"]["environment"].pop("HF_HUB_OFFLINE")
    with pytest.raises(ContractError, match="offline loading"):
        validate_runtime_config(altered)
    local_config["endpoint"]["binding_path"] = "binding.json"
    with pytest.raises(ContractError, match="must occur together"):
        validate_runtime_config(local_config)


def test_toml_roundtrip_preserves_definition_and_resolves_new_assets(local_config, tmp_path):
    local_config["endpoint"].update(binding_path="binding.json", binding_sha256="6" * 64)
    path = tmp_path / "candidate.toml"
    path.write_text(render(local_config))
    loaded = load_config(path).config.to_dict()
    assert loaded["definition"] == "local-engine-config.v1"
    for section, key in (
        ("model", "local_path"),
        ("model", "assets_path"),
        ("model", "template_path"),
        ("engine", "installation_path"),
        ("endpoint", "binding_path"),
    ):
        assert Path(loaded[section][key]) == tmp_path / local_config[section][key]
    assert loaded["generation"] == {"temperature": 0, "max_tokens": 512}


def test_single_plan_preserves_required_temperature_stops(local_config, config_path):
    bundle = load_config(config_path).bundle
    plan = compile_single_plan(local_config, bundle)
    assert plan["experiment"]["safety"] == {
        **local_config["safety"],
        "max_external_cpu_percent": None,
    }
    assert plan["experiment"]["workloads"][0]["protocol"]["kind"] == "fixed"


def test_old_configuration_is_not_promoted_to_local_profile(config_path):
    config = load_config(config_path).config.to_dict()
    validate_runtime_config(config)
    assert "definition" not in config
    assert "sha256" in config["model"]
    assert "content_sha256" not in config["model"]
