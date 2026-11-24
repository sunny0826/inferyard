"""Fail closed on upstream release, cache protocol and slots endpoint evidence."""

import asyncio
import hashlib

import httpx
import pytest

from inferyard.adapters.llama_cpp import BUILD, COMPONENT, LlamaCppAdapter
from inferyard.config.loader import load_config, validate_runtime_config
from inferyard.errors import PreflightError


def inputs(config_path):
    config = load_config(config_path).config.to_dict()
    config["engine"].update(adapter=COMPONENT, release=BUILD, slots_debug=True)
    config["generation"]["seed_support"] = "supported"
    config["engine"]["startup_args"] = [
        "-ngl",
        "0",
        "-t",
        str(config["conditions"]["threads"]),
        "-tb",
        str(config["conditions"]["threads_batch"]),
        "--reasoning",
        config["generation"]["reasoning_mode"],
        "--no-cache-prompt",
        "--no-cache-idle-slots",
        "--cache-ram",
        "0",
    ]
    validate_runtime_config(config)
    return config


@pytest.mark.parametrize("mutation", ["release", "cache", "build", "slots_endpoint"])
def test_upstream_identity_cannot_inherit_prism_qualification(config_path, mutation):
    config = inputs(config_path)
    props = {
        "build_info": BUILD,
        "total_slots": 1,
        "default_generation_settings": {"n_ctx": config["conditions"]["context_size"]},
        "model_path": config["model"]["local_path"],
        "endpoint_slots": True,
    }
    if mutation == "release":
        config["engine"]["release"] = "prism-b10743-adfffbe"
    elif mutation == "build":
        props["build_info"] = "b10743-adfffbe41"
    elif mutation == "slots_endpoint":
        props["endpoint_slots"] = False

    async def check():
        adapter = LlamaCppAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(lambda _: httpx.Response(200, json=props)),
        )
        if mutation == "cache":
            adapter.cache_protocol = "prism_prefix_reuse.v1"
        if mutation == "slots_endpoint":
            template = "upstream-fixture"
            props["chat_template"] = template
            config["model"]["template_sha256"] = hashlib.sha256(template.encode()).hexdigest()
        try:
            with pytest.raises(PreflightError):
                await adapter.verify_properties(config)
        finally:
            await adapter.close()

    asyncio.run(check())
