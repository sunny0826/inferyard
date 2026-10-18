"""The benchmark recipe preserves platform argv and resource stop thresholds."""

from copy import deepcopy

import pytest

from inferyard.config.context_speed_candidates import apply_recipe
from inferyard.config.context_speed_profile import (
    CASES,
    GENERATION,
    PROFILE,
    classify_workload_inputs,
)
from inferyard.config.preparation_io import PreparationError
from tests.unit.test_context_speed_profile import inputs as profile_inputs

inputs = profile_inputs


@pytest.mark.parametrize("context", CASES)
@pytest.mark.parametrize("backend", ["cpu", "metal", "cuda"])
def test_recipe_after_platform_defaults(inputs, context, backend):
    experiment, config, bundle = inputs
    original = deepcopy(config["output"])
    config["engine"]["backend"] = backend
    if backend == "cuda":
        config["engine"]["startup_args"] += ["--device", "CUDA2", "--log-verbosity", "4"]
    apply_recipe(
        config, {"general.architecture": "fixture", "fixture.context_length": 262144}, context
    )
    workload = experiment["workloads"][0]
    workload["protocol"]["case_ids"] = [CASES[context]]
    assert classify_workload_inputs(workload, config, bundle, experiment_profile=PROFILE) == PROFILE
    assert config["generation"] == GENERATION
    assert config["output"] == original
    if backend == "cuda":
        assert config["engine"]["startup_args"][-4:] == [
            "--device",
            "CUDA2",
            "--log-verbosity",
            "4",
        ]


@pytest.mark.parametrize("maximum", [None, 8192, True])
def test_model_context_refused(inputs, maximum):
    _, config, _ = inputs
    original = deepcopy(config)
    with pytest.raises(PreparationError, match="model_context_unsupported"):
        apply_recipe(
            config, {"general.architecture": "fixture", "fixture.context_length": maximum}, 65536
        )
    assert config == original


def test_macos_create_applies_recipe_after_fresh_recommendation(tmp_path, monkeypatch):
    from dataclasses import replace
    from pathlib import Path

    from inferyard.config import community_candidates, preparation_io
    from inferyard.config.loader import load_config
    from tests.unit.test_community_candidates import candidate_request
    from tests.unit.test_device_preflight import gguf

    monkeypatch.setattr(preparation_io, "native_platform", lambda: ("Darwin", "arm64"))
    monkeypatch.setattr(
        "inferyard.platforms.identity.environment_snapshot",
        lambda: {"platform": "Darwin", "profile": "balanced"},
    )
    model = gguf(tmp_path / "chosen.gguf", "chosen")
    engine = tmp_path / "llama-server"
    engine.write_bytes(b"synthetic, never executed")
    (tmp_path / "libggml-metal.dylib").write_bytes(b"library")
    request = replace(
        candidate_request(tmp_path, model, engine=engine, system="Darwin"),
        bundle_path=Path(__file__).parents[2] / "bundles/zh-context-speed.json",
        benchmark_profile="context-speed",
        context_size=131072,
    )
    monkeypatch.setattr(community_candidates, "require_review", lambda _: None)
    monkeypatch.setattr(
        community_candidates,
        "fresh_macos",
        lambda selected, _: dict(selected, context_size=2048, threads=3),
    )
    monkeypatch.setattr(community_candidates, "query_version", lambda *a, **k: {})
    monkeypatch.setattr(
        community_candidates,
        "model_template",
        lambda _: (
            {
                "general.name": "chosen",
                "general.architecture": "fixture",
                "fixture.context_length": 262144,
            },
            b"fixture-template",
        ),
    )
    result = community_candidates.create(request)
    config = load_config(result["candidate"]).config.to_dict()
    assert config["conditions"]["context_size"] == 131072
    assert config["conditions"]["threads"] == 3
    assert config["generation"] == GENERATION
    assert config["conditions"]["profile"] == "balanced"
    assert config["output"]["min_available_memory_bytes"] == 1073741824
    assert config["output"]["min_disk_bytes"] == 5368709120
    assert config["engine"]["startup_args"][9] == "131072"
