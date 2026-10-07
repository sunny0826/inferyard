import copy
import hashlib
import runpy
from pathlib import Path

import pytest

from inferyard.config.bundle import require_review
from inferyard.config.loader import load_config
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.length_evidence import REQUIRED, read_preparation
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, sha256_file


def seal(root):
    (root / "preparation-manifest.json").write_bytes(
        json_bytes({"files": {name: sha256_file(root / name) for name in REQUIRED}})
    )


def fixture(root, config_path):
    root.mkdir()
    config = load_config(config_path).config.to_dict()
    template = "test template"
    config["model"]["template_sha256"] = hashlib.sha256(template.encode()).hexdigest()
    spec = {
        "prefix": "开始",
        "suffix": "结束",
        "padding_unit": "填",
        "target_tokens": 10,
        "tolerance_tokens": 1,
        "max_repetitions": 100,
        "max_probes": 32,
        "max_characters": 100,
        "max_wall_seconds": 10,
    }
    prompt = "开始填结束"
    record = {
        "repetitions": 1,
        "prompt_sha256": hashlib.sha256(prompt.encode()).hexdigest(),
        "measurement": {
            "input_tokens": 10,
            "output_budget": config["generation"]["max_tokens"],
            "verification": "verified",
            "source": "apply-template+tokenize:add_special,parse_special",
            "template_prompt_sha256": "a" * 64,
        },
    }
    values = {
        "spec.json": spec,
        "config.frozen.json": config,
        "service-binding.json": {},
        "service.props.json": {
            "chat_template": template,
            "model_path": config["model"]["local_path"],
        },
        "identity.json": {
            "verification": "verified",
            "files": [
                {"path": config[section][path_key], "sha256": config[section][hash_key]}
                for section, path_key, hash_key in (
                    ("model", "local_path", "sha256"),
                    ("model", "template_path", "template_sha256"),
                    ("engine", "binary_path", "binary_sha256"),
                )
            ],
        },
        "probes.json": [record],
        "result.json": {
            "status": "matched",
            "selected": record,
            "probes": [record],
            "scope": "model_specific_performance_text_not_shared_quality_corpus",
        },
    }
    for name, value in values.items():
        (root / name).write_bytes(json_bytes(value))
    (root / "prompt.txt").write_text(prompt)
    seal(root)
    return root


def test_preparation_detects_raw_tamper_and_resealed_semantic_mismatch(tmp_path, config_path):
    root = fixture(tmp_path / "prep", config_path)
    assert read_preparation(root)["prompt"] == "开始填结束"
    (root / "prompt.txt").write_text("different prompt")
    with pytest.raises(EvidenceError, match="hash_mismatch"):
        read_preparation(root)
    seal(root)
    with pytest.raises(EvidenceError, match="target_mismatch"):
        read_preparation(root)


@pytest.mark.parametrize("mutation", ["selected", "count", "source", "inventory", "repetitions"])
def test_preparation_rejects_invalid_resealed_records(tmp_path, config_path, mutation):
    root = fixture(tmp_path / "prep", config_path)
    probes = read_json(root / "probes.json")
    result = read_json(root / "result.json")
    if mutation == "inventory":
        manifest = read_json(root / "preparation-manifest.json")
        manifest["files"]["../escape"] = "a" * 64
        (root / "preparation-manifest.json").write_bytes(json_bytes(manifest))
    else:
        if mutation == "count":
            probes[0]["measurement"]["input_tokens"] = True
        elif mutation == "source":
            probes[0]["measurement"]["source"] = "guessed"
        elif mutation == "repetitions":
            probes[0]["repetitions"] = 101
        result["probes"] = probes
        result["selected"] = copy.deepcopy(probes[0])
        if mutation == "selected":
            result["selected"]["repetitions"] = 2
        (root / "probes.json").write_bytes(json_bytes(probes))
        (root / "result.json").write_bytes(json_bytes(result))
        seal(root)
    with pytest.raises(EvidenceError):
        read_preparation(root)


def test_scan_freezes_cartesian_budget_without_fabricating_review(tmp_path, config_path):
    root = fixture(tmp_path / "prep", config_path)
    build = runpy.run_path(str(Path(__file__).parents[2] / "scripts/prepare_length_scan.py"))[
        "prepare_scan"
    ]
    out = tmp_path / "scan"
    review = build([root], [16, 32], 5, out)
    plan, loaded = read_frozen_plan(out / "frozen/plan.json")
    assert plan["request_limit"] == 10
    assert len(plan["trials"]) == 10
    assert review["independent_quality_questions"] == 0
    assert all(w["input_tolerance_tokens"] == 1 for w in plan["experiment"]["workloads"])
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(next(iter(loaded.values())).bundle.to_dict())
    with pytest.raises(EvidenceError, match="duplicate_target"):
        build([root, root], [16], 1, tmp_path / "duplicate")
    with pytest.raises(EvidenceError, match="context_budget"):
        build([root], [1000000], 1, tmp_path / "overflow")


def test_preparation_rejects_config_model_without_matching_identity(tmp_path, config_path):
    root = fixture(tmp_path / "prep", config_path)
    config = read_json(root / "config.frozen.json")
    config["model"]["sha256"] = "b" * 64
    (root / "config.frozen.json").write_bytes(json_bytes(config))
    seal(root)
    with pytest.raises(EvidenceError, match="identity_mismatch"):
        read_preparation(root)
