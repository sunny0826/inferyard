import hashlib
import json

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.quantization_comparison import qualification
from tests.integration.test_lineage_records import encoded
from tests.unit.test_comparison import trial
from tests.unit.test_lineage_files import fixture


def pair(tmp_path):
    values = []
    for side, packing in [("a", "Q4_K_M"), ("b", "Q8_0")]:
        root = tmp_path / side
        root.mkdir()
        workload, files = fixture(root)
        for role in ["output", "quantization_recipe"]:
            content = (side + role).encode()
            (root / files[role]).write_bytes(content)
            workload["model_lineage"][role + "_sha256"] = hashlib.sha256(content).hexdigest()
        workload["model_lineage"]["packing"] = packing
        receipt = json.loads(workload["model_lineage_records"]["quantization"]["text"])
        receipt.update(
            packing=packing,
            output_sha256=workload["model_lineage"]["output_sha256"],
            recipe_sha256=workload["model_lineage"]["quantization_recipe_sha256"],
        )
        workload["model_lineage_records"]["quantization"] = encoded(receipt)
        data = trial()
        data["plan"]["experiment"]["comparison"] = {"mode": "config", "factor": "quantization"}
        data["plan"]["experiment"]["workloads"][0].update(workload)
        data["config"]["model"].update(
            sha256=workload["model_lineage"]["output_sha256"], packing=packing
        )
        data["config"]["quantization_artifact_binding"] = {"root": str(root), "files": files}
        data["config"]["engine"]["startup_args"] += ["-m", str(root / files["output"])]
        values.append(data)
    return values


def test_byte_verified_same_base_can_compare_quality_without_granting_performance(tmp_path):
    left, right = pair(tmp_path)
    result = compare_trials(left, right)
    assert result["quantization_qualification"]["eligible"]
    assert result["eligibility"]["quality"] and not result["eligibility"]["performance"]


@pytest.mark.parametrize(
    "change",
    ["artifact", "tokenizer", "revision", "receipt", "second_factor", "same_packing", "bindings"],
)
def test_missing_tampered_or_multifactor_proof_refuses_deltas(tmp_path, change):
    left, right = pair(tmp_path)
    work = right["plan"]["experiment"]["workloads"][0]
    if change == "artifact":
        from pathlib import Path

        binding = right["config"]["quantization_artifact_binding"]
        (Path(binding["root"]) / binding["files"]["output"]).write_bytes(b"tampered")
    if change == "tokenizer":
        work["model_lineage"]["tokenizer_sha256"] = "1" * 64
    if change == "revision":
        work["model_lineage"]["base_revision"] = "1" * 40
    if change == "receipt":
        work["model_lineage_records"]["quantization"]["sha256"] = "1" * 64
    if change == "second_factor":
        right["config"]["conditions"]["threads"] = 8
    if change == "same_packing":
        right["config"]["model"]["packing"] = left["config"]["model"]["packing"]
    if change == "bindings":
        right["config"].pop("quantization_artifact_binding")
    result = compare_trials(left, right)
    assert not result["eligibility"]["quality"]
    assert result["completion_rate_difference"] is None


def test_receipts_and_manual_verified_boolean_cannot_replace_file_bytes(tmp_path):
    left, right = pair(tmp_path)
    right["config"]["quantization_artifact_binding"]["files"].clear()
    right["identity"]["lineage_verified"] = True
    assert not qualification(left, right)["eligible"]


def test_v3_reuses_verified_lineage_once_for_tokenizer_rate(tmp_path, monkeypatch):
    from copy import deepcopy

    from inferyard.analysis import quantization_comparison as module
    from inferyard.implementation_identity import IdentityContext
    from tests.unit.test_performance_comparison import inputs

    left, right = pair(tmp_path)
    a, b, proofs = inputs()
    identity = IdentityContext().value
    for data, performance in zip((left, right), (a, b), strict=True):
        for key in ("requests", "summary", "environment_start"):
            data[key] = deepcopy(performance[key])
        data["summary"]["counts"] = {"planned": 1, "valid_executed": 1}
        data["bundle"] = {
            "cases": [{"case_id": "case"}],
            "answer_policy": {},
            "task_protocol": "quality",
        }
        data["run"].update(run_id=performance["run"]["run_id"], implementation_identity=identity)
        data["run"]["definition_versions"]["scoring"] = "phase2.v1"
    calls = []
    original = module.verify_lineage_files

    def counted(*args, **kwargs):
        calls.append(1)
        return original(*args, **kwargs)

    monkeypatch.setattr(module, "verify_lineage_files", counted)
    result = compare_trials(left, right, definition="phase2.v3", performance_evidence=proofs)
    assert calls == [1, 1]  # One per side; performance consumes the returned qualification.
    rows = {r["metric_id"]: r for r in result["performance_analysis"]["differences"]}
    assert rows["L04"]["eligible"], result["performance_analysis"]
    assert rows["L04"]["difference"] == 20
    assert result["quantization_qualification"]["eligible"]
    right["config"]["model"]["template_sha256"] = "other-template"
    refused = compare_trials(left, right, definition="phase2.v3", performance_evidence=proofs)
    rate = next(
        r for r in refused["performance_analysis"]["differences"] if r["metric_id"] == "L04"
    )
    assert rate["difference"] is None and "tokenizer_identity_not_proven_equal" in rate["reasons"]
