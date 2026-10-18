"""Optional profile must not change ordinary frozen plans."""

import hashlib
import json
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.config.bundle import content_hash, require_review
from inferyard.config.context_speed_profile import (
    BUNDLE_CONTENT_SHA256,
    CASES,
    GENERATION,
    PROFILE,
    classify_workload_inputs,
)
from inferyard.config.loader import load_config
from inferyard.config.plan_inputs import read_frozen_plan, validate_workload_inputs
from inferyard.config.planning import compile_plan, write_plan
from inferyard.contracts.validation import ContractError, validate_document
from inferyard.evidence.storage import json_bytes
from tests.unit.test_phase2_contracts import experiment


def speed_experiment():
    data = experiment()
    data["profile"] = "context-speed.v1"
    data["workloads"][0].update(
        purpose="performance",
        repeats=5,
        output_budget_tokens=256,
        output_mode="strict_fixed_length",
        protocol={"kind": "fixed", "case_ids": ["context-speed-64k"]},
    )
    return data


def test_profile_plan():
    plan = compile_plan(speed_experiment())
    assert len(plan["trials"]) == 5


@pytest.fixture
def inputs(config_path):
    config = load_config(config_path).config.to_dict()
    config["bundle"] = {"path": "bundle.json", "version": "2026.10.1"}
    config["conditions"].update(context_size=65536, cache_policy="disabled")
    config["generation"] = deepcopy(GENERATION)
    config["execution"].update(warmup_count=1, timeout_seconds=10)
    config["engine"]["startup_args"] = [
        "-m",
        "fixture.gguf",
        "-c",
        "65536",
        "-np",
        "1",
        "--reasoning",
        "off",
        "--no-cache-prompt",
        "--no-cache-idle-slots",
        "--cache-ram",
        "0",
    ]
    bundle = json.loads(
        (Path(__file__).resolve().parents[2] / "bundles/zh-context-speed.json").read_text()
    )
    return speed_experiment(), config, bundle


def test_draft_hash_and_review_are_separate(inputs):
    experiment, config, bundle = inputs
    assert content_hash(bundle) == BUNDLE_CONTENT_SHA256
    assert bundle["review_records"] == []
    validate_document("bundle", bundle)
    validate_workload_inputs(experiment["workloads"][0], config, bundle, experiment_profile=PROFILE)
    with pytest.raises(ContractError):
        require_review(bundle)


@pytest.mark.parametrize("context", [65536, 131072, 262144])
def test_all_three_fixed_contexts(inputs, context):
    experiment, config, bundle = inputs
    config["conditions"]["context_size"] = context
    config["engine"]["startup_args"][3] = str(context)
    workload = experiment["workloads"][0]
    workload["protocol"]["case_ids"] = [CASES[context]]
    validate_workload_inputs(workload, config, bundle, experiment_profile=PROFILE)
    assert classify_workload_inputs(workload, config, bundle, experiment_profile=PROFILE) == PROFILE


@pytest.mark.parametrize("profile", [None, "unknown", "context-speed.v2"])
def test_special_cannot_fall_back_to_ordinary(inputs, profile):
    experiment, config, bundle = inputs
    with pytest.raises(ContractError):
        validate_workload_inputs(
            experiment["workloads"][0], config, bundle, experiment_profile=profile
        )


@pytest.mark.parametrize(
    "field,value",
    [
        ("repeats", 4),
        ("purpose", "quality"),
        ("input_target_tokens", 1000),
        ("output_budget_tokens", 255),
    ],
)
def test_workload_recipe_rejected(inputs, field, value):
    experiment, _, _ = inputs
    experiment["workloads"][0][field] = value
    with pytest.raises(ContractError):
        compile_plan(experiment)


@pytest.mark.parametrize(
    "section,field,value",
    [
        ("conditions", "context_size", 32768),
        ("conditions", "cache_policy", "enabled"),
        ("conditions", "slots", 2),
        ("generation", "seed", 42),
        ("generation", "seed", False),
        ("generation", "stop", ["END"]),
        ("generation", "max_tokens", 128),
        ("generation", "seed_support", "unknown"),
        ("execution", "warmup_count", 3),
        ("execution", "require_fresh_process", True),
        ("engine", "adapter", "llama_cpp_b11146_v1"),
    ],
)
def test_config_recipe_rejected(inputs, section, field, value):
    experiment, config, bundle = inputs
    config[section][field] = value
    with pytest.raises(ContractError):
        validate_workload_inputs(
            experiment["workloads"][0], config, bundle, experiment_profile=PROFILE
        )


@pytest.mark.parametrize(
    "args",
    [
        ["--draft-max", "5"],
        ["--model-draft", "other.gguf"],
        ["--spec-type", "ngram"],
        ["-np", "2"],
        ["--cache-prompt"],
    ],
)
def test_startup_conflicts_rejected(inputs, args):
    experiment, config, bundle = inputs
    config["engine"]["startup_args"] += args
    with pytest.raises(ContractError):
        validate_workload_inputs(
            experiment["workloads"][0], config, bundle, experiment_profile=PROFILE
        )


def test_same_name_different_body_or_policy_rejected(inputs):
    experiment, config, bundle = inputs
    for field in ("prompt", "rules"):
        bad = deepcopy(bundle)
        if field == "prompt":
            bad["cases"][0][field] += "different"
        else:
            bad["cases"][0][field]["output_target_tokens"] = 255
        with pytest.raises(ContractError, match="bundle_content_mismatch"):
            validate_workload_inputs(
                experiment["workloads"][0], config, bad, experiment_profile=PROFILE
            )


def test_ordinary_plan_hash_unchanged():
    plain = experiment()
    before = json_bytes(plain)
    plan = compile_plan(plain)
    assert plan["plan_sha256"] == "1b25a7d95fba8a95ea3c13e36b7ceabbdd21fb5cdc156143c4d0cb1ddc75b250"
    assert json_bytes(plain) == before
    assert "profile" not in plan["experiment"]


def test_prepare_and_offline_read_share_profile_validation(inputs, tmp_path):
    experiment, config, bundle = inputs
    source = tmp_path / "source"
    source.mkdir()
    for kind, data in (("config", config), ("bundle", bundle)):
        raw = json_bytes(data)
        (source / f"{kind}.json").write_bytes(raw)
        experiment["workloads"][0][kind] = {
            "path": f"{kind}.json",
            "sha256": hashlib.sha256(raw).hexdigest(),
        }
    path = source / "experiment.json"
    path.write_bytes(json_bytes(experiment))
    out = tmp_path / "frozen"
    written = write_plan(path, out)
    loaded, _ = read_frozen_plan(out / "plan.json")
    assert loaded == written
    # A validly rehashed ordinary declaration cannot hide the special bundle.
    from inferyard.config.plan_math import plan_hash

    loaded["experiment"].pop("profile")
    loaded["plan_sha256"] = plan_hash(loaded)
    (out / "plan.json").write_bytes(json_bytes(loaded))
    with pytest.raises(ContractError, match="profile_missing"):
        read_frozen_plan(out / "plan.json")
    experiment.pop("profile")
    path.write_bytes(json_bytes(experiment))
    with pytest.raises(ContractError, match="profile_missing"):
        write_plan(path, tmp_path / "rejected")
    assert not (tmp_path / "rejected").exists()


@pytest.mark.parametrize(
    "profile,reason", [(None, "profile_missing"), (PROFILE, "case_context_mismatch")]
)
def test_run_input_read_rejects_conflicting_profile(inputs, tmp_path, profile, reason):
    from types import SimpleNamespace

    from inferyard.evidence.ledger import _inputs

    experiment, config, bundle = inputs
    if profile is None:
        experiment.pop("profile")
    plan = compile_plan(experiment)
    trial = plan["trials"][0]
    # Layout2 TrialEvidence normally validates these envelopes before _inputs.
    # This controlled reader isolates the actual ledger input admission path.
    run = {
        "run_id": "run-1",
        "trial_id": trial["trial_id"],
        "experiment_id": experiment["experiment_id"],
        "plan_sha256": plan["plan_sha256"],
        "definition_versions": experiment["definition_versions"],
        "relation": "initial",
    }
    if profile is not None:
        config["conditions"]["context_size"] = 131072
    documents = {
        "run": run,
        "plan": plan,
        "config": config,
        "bundle": bundle,
        "selection": {"run_id": "run-1", "trial_id": trial["trial_id"]},
    }
    for document in documents.values():
        document["schema_version"] = 3
    reads = SimpleNamespace(layout_version=2, input=documents.__getitem__)
    with pytest.raises(ContractError, match=reason):
        _inputs(tmp_path, reads, [])


def test_ordinary_inputs_keep_behavior(config_path):
    from inferyard.config.single_plan import compile_single_plan

    loaded = load_config(config_path)
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    workload = plan["experiment"]["workloads"][0]
    assert classify_workload_inputs(workload, config, bundle) is None
    validate_workload_inputs(workload, config, bundle)


def test_rehashed_trial_reorder_is_rejected():
    from inferyard.config.plan_math import plan_hash

    plan = compile_plan(speed_experiment())
    plan["trials"].reverse()
    plan["plan_sha256"] = plan_hash(plan)
    with pytest.raises(ContractError, match="frozen repeat order"):
        validate_document("plan", plan)
