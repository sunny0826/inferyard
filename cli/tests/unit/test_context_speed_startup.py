"""Startup compatibility uses synthetic assets and the real Windows recipe, without IO."""

from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.config.community_candidates_windows import build_config
from inferyard.config.context_speed_profile import (
    CASES,
    GENERATION,
    PROFILE,
    classify_workload_inputs,
)
from inferyard.contracts.validation import ContractError, validate_document
from tests.unit import test_context_speed_profile

inputs = test_context_speed_profile.inputs


def classify(inputs):
    experiment, config, bundle = inputs
    return classify_workload_inputs(
        experiment["workloads"][0], config, bundle, experiment_profile=PROFILE
    )


@pytest.mark.parametrize("context", [65536, 131072, 262144])
@pytest.mark.parametrize("mode,gpu_index", [("cpu", None), ("cuda", 0), ("cuda", 1)])
def test_windows_full_recipe(inputs, context, mode, gpu_index):
    experiment, _, bundle = inputs
    config = build_config(
        {
            "mode": mode,
            "gpu_index": gpu_index,
            "threads": 4,
            "context_size": context,
            "min_available_memory_bytes": 8 * 1024**3,
            "min_disk_bytes": 5 * 1024**3,
        },
        SimpleNamespace(path="fixture.gguf", sha256="a" * 64),
        {"general.name": "Synthetic model"},
        "synthetic template",
        Path("template.jinja"),
        SimpleNamespace(path="fixture.exe", sha256="b" * 64),
        {"release": "synthetic", "runtime_library_manifest": "libraries.json"},
        Path("bundle.json"),
        "2026.10.1",
        Path("results"),
    )
    original_args = list(config["engine"]["startup_args"])
    config["generation"] = deepcopy(GENERATION)
    config["execution"].update(repeats=1, concurrency=1, warmup_count=1, order="fixed")
    experiment["workloads"][0]["protocol"]["case_ids"] = [CASES[context]]
    validate_document("config", config)
    validate_document("bundle", bundle)
    print(f"{mode=} {gpu_index=} {context=} argv={original_args!r}")
    try:
        assert classify((experiment, config, bundle)) == PROFILE
    finally:
        assert config["engine"]["startup_args"] == original_args


@pytest.mark.parametrize("backend", ["cpu", "cuda", "metal"])
def test_existing_recipe_without_optional_arguments(inputs, backend):
    inputs[1]["engine"]["backend"] = backend
    before = deepcopy(inputs[1])
    assert classify(inputs) == PROFILE
    assert inputs[1] == before


@pytest.mark.parametrize("device", ["CUDA0", "CUDA1", "CUDA2", "CUDA10", "CUDA123"])
@pytest.mark.parametrize("device_inline", [False, True])
@pytest.mark.parametrize("verbosity_inline", [False, True])
def test_optional_forms(inputs, device, device_inline, verbosity_inline):
    engine = inputs[1]["engine"]
    engine["backend"] = "cuda"
    engine["startup_args"] += [f"--device={device}"] if device_inline else ["--device", device]
    engine["startup_args"] += (
        ["--log-verbosity=4"] if verbosity_inline else ["--log-verbosity", "4"]
    )
    before = deepcopy(engine)
    assert classify(inputs) == PROFILE
    assert engine == before


@pytest.mark.parametrize("option,value", [("--device", "CUDA0"), ("--log-verbosity", "4")])
@pytest.mark.parametrize("form", ["missing", "empty", "inline-empty", "long-option"])
def test_missing_values(inputs, option, value, form):
    args = {
        "missing": [option],
        "empty": [option, ""],
        "inline-empty": [option + "="],
        "long-option": [option, "--slots"],
    }[form]
    assert_rejected(inputs, args, "startup_value_missing")


@pytest.mark.parametrize("option,value", [("--device", "CUDA0"), ("--log-verbosity", "4")])
@pytest.mark.parametrize(
    "first_inline,second_inline", [(False, False), (True, True), (False, True), (True, False)]
)
def test_duplicates(inputs, option, value, first_inline, second_inline):
    first = [f"{option}={value}"] if first_inline else [option, value]
    second = [f"{option}={value}"] if second_inline else [option, value]
    assert_rejected(inputs, first + second, "startup_option_ambiguous")


@pytest.mark.parametrize("inline", [False, True])
@pytest.mark.parametrize(
    "value",
    [
        "CUDA-1",
        "CUDA01",
        "CUDA0x",
        "CUDA0 ",
        "CUDA０",
        "CUDA١",
        "CUDA0,CUDA1",
        "CUDA0 CUDA1",
        "cpu",
        "metal",
        "CUDA",
        "-1",
    ],
)
def test_invalid_devices(inputs, inline, value):
    args = [f"--device={value}"] if inline else ["--device", value]
    assert_rejected(inputs, args, "startup_recipe_mismatch")


@pytest.mark.parametrize("inline", [False, True])
@pytest.mark.parametrize("value", ["0", "3", "5", "04", "4.0", "4 ", "４", "-1"])
def test_invalid_verbosity(inputs, inline, value):
    args = [f"--log-verbosity={value}"] if inline else ["--log-verbosity", value]
    assert_rejected(inputs, args, "startup_recipe_mismatch")


@pytest.mark.parametrize("backend", ["cpu", "metal"])
@pytest.mark.parametrize("args", [["--device", "CUDA0"], ["--device=CUDA10"]])
def test_device_requires_cuda(inputs, backend, args):
    assert_rejected(inputs, args, "startup_recipe_mismatch", backend=backend)


@pytest.mark.parametrize(
    "args,reason",
    [
        (["--device", "-c", "65536"], "startup_recipe_mismatch"),
        (["--device", "--port"], "startup_value_missing"),
        (["--device", "-1"], "startup_recipe_mismatch"),
        (["--log-verbosity", "--slots"], "startup_value_missing"),
        (["--unknown-option", "4"], "startup_option_unsupported"),
    ],
)
def test_swallowed_options_and_unknown(inputs, args, reason):
    # Appending -c duplicates the required context: the existing recipe check fails first.
    assert_rejected(inputs, args, reason)


def test_device_occupying_the_only_context_option(inputs):
    args = inputs[1]["engine"]["startup_args"]
    args.insert(args.index("-c"), "--device")
    assert_rejected(inputs, [], "startup_option_ambiguous")


def assert_rejected(inputs, args, reason, *, backend="cuda"):
    engine = inputs[1]["engine"]
    engine["backend"] = backend
    engine["startup_args"] += args
    before = deepcopy(engine)
    with pytest.raises(ContractError) as error:
        classify(inputs)
    assert error.value.path == "context_speed.profile"
    assert error.value.reason == reason
    assert engine == before
