"""Formal profiles retain asset meanings and reject undeclared capabilities."""

from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.config.loader import load_config, validate_runtime_config
from inferyard.config.single_plan import compile_single_plan
from inferyard.config.toml_writer import render
from inferyard.errors import ContractError


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
        "check_environment": True,
    }
    return config
