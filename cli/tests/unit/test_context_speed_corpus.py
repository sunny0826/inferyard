"""Offline corpus identity, reproducibility and real admission/refusal paths."""

import copy
import hashlib
import json
import subprocess
import sys
from pathlib import Path

import pytest

from inferyard.config.bundle import content_hash, require_review, validate_bundle
from inferyard.config.bundle_review import case_content_hash
from inferyard.contracts.validation import ContractError
from scripts.build_context_speed_bundle import (
    BUNDLE_NAME,
    MARKDOWN_NAME,
    REVIEW_NAME,
    build,
    outputs,
    write_outputs,
)
from scripts.context_speed_corpus import record

ROOT = Path(__file__).resolve().parents[2]


def test_fixed_three_cases_and_output_recipe():
    bundle = build()
    validate_bundle(bundle)
    assert (bundle["schema_version"], bundle["bundle_id"], bundle["version"]) == (
        3,
        "zh-context-speed",
        "2026.10.1",
    )
    assert bundle["task_protocol"] == "performance"
    assert [case["case_id"] for case in bundle["cases"]] == [
        "context-speed-64k",
        "context-speed-128k",
        "context-speed-256k",
    ]
    for case, count in zip(bundle["cases"], (71, 144, 290), strict=True):
        assert case["category"] == "performance"
        assert case["reference_answer"] == ""
        assert case["rules"] == {"output_target_tokens": 256}
        assert case["prompt"].count("### 档案") == count
    review = json.loads(outputs()[REVIEW_NAME])
    assert [c["context_tokens"] for c in review["cases"]] == [65536, 131072, 262144]
    assert [c["reference_workload_target_tokens"] for c in review["cases"]] == [
        52625,
        106294,
        213612,
    ]
    assert review["execution_recipe_status"] == (
        "planning_reference_only_not_executed_or_enforced_by_bundle"
    )


def test_repeatable_bytes_and_checked_in_artifacts():
    first = outputs()
    assert outputs() == first
    for name, raw in first.items():
        assert (ROOT / "bundles" / name).read_bytes() == raw


def test_material_has_multiple_topics_and_structures_without_duplicate_records():
    records = [record(i) for i in range(512)]
    assert len(set(records)) == 512
    text = "\n".join(records[:72])
    for marker in (
        "河道观测",
        "手工织造",
        "古籍修复",
        "过程叙述",
        "批次表",
        "访谈摘记",
        "结构化摘录",
        "工作步骤",
        "方案对照",
    ):
        assert marker in text


def test_review_hashes_and_missing_token_evidence():
    data = outputs()
    bundle = json.loads(data[BUNDLE_NAME])
    review = json.loads(data[REVIEW_NAME])
    assert review["human_review"] == "pending"
    assert review["bundle_file_sha256"] == hashlib.sha256(data[BUNDLE_NAME]).hexdigest()
    assert review["bundle_content_sha256"] == content_hash(bundle)
    for case, entry in zip(bundle["cases"], review["cases"], strict=True):
        raw = case["prompt"].encode()
        assert entry["prompt_characters"] == len(case["prompt"])
        assert entry["prompt_utf8_bytes"] == len(raw)
        assert entry["prompt_sha256"] == hashlib.sha256(raw).hexdigest()
        assert entry["case_content_sha256"] == case_content_hash(bundle, case)
        assert entry["reference_tokenizer"] == "Prism llama-tokenize"
        assert entry["reference_model"] == "SmolLM2-135M-Instruct-Q4_K_M.gguf"
        assert len(entry["reference_tokenizer_sha256"]) == 64
        assert len(entry["reference_model_sha256"]) == 64
        assert entry["reference_count_scope"] == "raw_prompt_no_chat_template_no_bos"
        assert entry["reference_model_native_context_tokens"] == 8192
        assert entry["actual_input_tokens"] is None
        assert (
            abs(entry["reference_prompt_tokens"] - entry["reference_workload_target_tokens"]) < 400
        )

        assert entry["token_count_missing_reason"]


@pytest.mark.parametrize(
    "field,value",
    [
        ("prompt", "修改后的正文"),
        ("reference_answer", "新答案"),
        ("rules", {"output_target_tokens": 255}),
    ],
)
def test_changed_content_invalidates_hash_binding(field, value):
    original = build()
    changed = copy.deepcopy(original)
    changed["cases"][0][field] = value
    assert content_hash(changed) != content_hash(original)
    assert case_content_hash(changed, changed["cases"][0]) != case_content_hash(
        original, original["cases"][0]
    )


def test_real_review_gate_rejects_unapproved_corpus():
    bundle = build()
    assert bundle["review_records"] == bundle["case_review_records"] == []
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)


@pytest.mark.parametrize("name", [BUNDLE_NAME, REVIEW_NAME, MARKDOWN_NAME])
def test_any_existing_output_blocks_all_writes(tmp_path, name):
    existing = tmp_path / name
    existing.write_bytes(b"original bytes")
    with pytest.raises(FileExistsError, match="output_exists"):
        write_outputs(tmp_path)
    assert existing.read_bytes() == b"original bytes"
    assert list(tmp_path.iterdir()) == [existing]


def test_dangling_symlink_is_not_overwritten(tmp_path):
    link = tmp_path / BUNDLE_NAME
    link.symlink_to(tmp_path / "absent")
    with pytest.raises(FileExistsError, match="output_exists"):
        write_outputs(tmp_path)
    assert link.is_symlink()
    assert not (tmp_path / "absent").exists()


def test_cli_generates_then_refuses_overwrite_with_exit_2(tmp_path):
    command = [
        sys.executable,
        str(ROOT / "scripts/build_context_speed_bundle.py"),
        "--out-dir",
        str(tmp_path / "new"),
    ]
    first = subprocess.run(command, capture_output=True, text=True, check=False)
    assert first.returncode == 0, first.stderr
    assert json.loads(first.stdout) == {"human_review": "pending", "cases": 3, "model_requests": 0}
    before = {p.name: p.read_bytes() for p in (tmp_path / "new").iterdir()}
    second = subprocess.run(command, capture_output=True, text=True, check=False)
    assert second.returncode == 2
    assert "output_exists" in second.stderr
    assert before == {p.name: p.read_bytes() for p in (tmp_path / "new").iterdir()}


def test_changed_prompt_cannot_inherit_reference_count(monkeypatch):
    from scripts import build_context_speed_bundle as generator

    original = generator.prompt
    monkeypatch.setattr(generator, "prompt", lambda label, count: original(label, count) + "变化")
    with pytest.raises(ValueError, match="reference_token_count_prompt_hash_mismatch"):
        generator.outputs()
