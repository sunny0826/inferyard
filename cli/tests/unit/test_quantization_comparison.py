import hashlib
import json

import pytest

from inferyard.analysis.comparison import compare_trials
from tests.integration.test_lineage_records import encoded
from tests.unit.lineage_files_helpers import fixture
from tests.unit.test_performance_comparison import trial


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
