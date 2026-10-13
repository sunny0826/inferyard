"""Fail closed on upstream release, effective settings and template evidence."""

import asyncio
from copy import deepcopy

import httpx
import pytest

from inferyard.adapters.llama_cpp import BUILD, COMPONENT, LlamaCppAdapter
from inferyard.adapters.prism import ResponseState
from inferyard.config.loader import load_config, validate_runtime_config
from inferyard.platforms.identity import PreflightError
from inferyard.registry import adapter_factory


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


def slot(config):
    params = dict(config["generation"])
    params["n_predict"] = params["max_tokens"]
    return [{"id": 0, "is_processing": False, "params": params}]


def test_upstream_registered_and_effective_stop_is_observed(config_path):
    config = inputs(config_path)
    assert adapter_factory(COMPONENT) is LlamaCppAdapter

    async def check():
        adapter = LlamaCppAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(lambda _: httpx.Response(200, json=slot(config))),
        )
        try:
            result = await adapter.verify_effective(config, 12, ResponseState(0, prompt_tokens=12))
            assert result["template_count_matches_usage"]
            assert result["stop"] == {"value": [], "source": "slots.params.stop"}
        finally:
            await adapter.close()

    asyncio.run(check())


@pytest.mark.parametrize(
    "change,reason",
    [
        ({"n_predict": 999}, "generation_parameter_unverified"),
        ({"stop": ["unexpected"]}, "stop_parameter_unverified"),
        ({"seed": True}, "generation_parameter_unverified"),
    ],
)
def test_inconsistent_upstream_effective_settings_rejected(config_path, change, reason):
    config = inputs(config_path)
    observed = slot(config)
    observed[0]["params"].update(change)

    async def check():
        adapter = LlamaCppAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(lambda _: httpx.Response(200, json=observed)),
        )
        try:
            with pytest.raises(PreflightError, match=reason):
                await adapter.verify_effective(config, 12, ResponseState(0, prompt_tokens=12))
        finally:
            await adapter.close()

    asyncio.run(check())


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
        # Deliberately stop before the fixture's absent template fingerprint.
        if mutation == "slots_endpoint":
            import hashlib

            template = "upstream-fixture"
            props["chat_template"] = template
            config["model"]["template_sha256"] = hashlib.sha256(template.encode()).hexdigest()
        try:
            with pytest.raises(PreflightError):
                await adapter.verify_properties(config)
        finally:
            await adapter.close()

    asyncio.run(check())


def test_different_prompt_count_is_rejected(config_path):
    config = inputs(config_path)

    async def check():
        adapter = LlamaCppAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(lambda _: httpx.Response(200, json=slot(config))),
        )
        try:
            with pytest.raises(PreflightError, match="template_token_count_mismatch"):
                await adapter.verify_effective(config, 12, ResponseState(0, prompt_tokens=13))
        finally:
            await adapter.close()

    asyncio.run(check())


def test_unknown_upstream_version_requires_separate_adapter(config_path):
    config = deepcopy(inputs(config_path))
    config["engine"]["adapter"] = "llama_cpp_latest"
    from inferyard.contracts.validation import ContractError

    with pytest.raises(ContractError):
        validate_runtime_config(config)
