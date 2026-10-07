import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

from inferyard.contracts.validation import ContractError
from inferyard.evidence.lineage_files import verify_file, verify_lineage_files
from tests.integration.test_lineage_records import encoded, records
from tests.unit.test_model_lineage import lineage


def fixture(tmp_path):
    declared = lineage()
    bindings = {}
    for key in (
        "tokenizer",
        "conversion_tool",
        "conversion_recipe",
        "quantization_tool",
        "quantization_recipe",
        "output",
        "base:weights",
        "intermediate",
    ):
        content = (key * 100).encode()
        path = tmp_path / key.replace(":", "-")
        path.write_bytes(content)
        bindings[key] = path.name
        digest = hashlib.sha256(content).hexdigest()
        if key == "base:weights":
            declared["base_artifacts"][0]["sha256"] = digest
        elif key != "intermediate":
            declared[key + "_sha256"] = digest
    receipts = records(declared)
    digest = hashlib.sha256((tmp_path / "intermediate").read_bytes()).hexdigest()
    for name, key in (("conversion", "output_sha256"), ("quantization", "input_sha256")):
        record = json.loads(receipts[name]["text"])
        record[key] = digest
        receipts[name] = encoded(record)
    return {"model_lineage": declared, "model_lineage_records": receipts}, bindings


def test_file_identity_pass_is_not_authenticated_lineage(tmp_path):
    workload, bindings = fixture(tmp_path)
    result = verify_lineage_files(workload, bindings, tmp_path)
    assert result["all_files_verified"]
    assert len(result["files"]) == 8
    assert not result["lineage_authenticated"] and not result["comparison_eligible"]
    assert all(r["bytes"] > 0 and r["reason"] is None for r in result["files"])


@pytest.mark.parametrize(
    "artifact",
    [
        "base:weights",
        "tokenizer",
        "conversion_tool",
        "conversion_recipe",
        "quantization_tool",
        "quantization_recipe",
        "output",
        "intermediate",
    ],
)
@pytest.mark.parametrize("change", ["missing_binding", "missing_file", "wrong_bytes"])
def test_every_artifact_is_required_and_remains_in_denominator(tmp_path, artifact, change):
    workload, bindings = fixture(tmp_path)
    if change == "missing_binding":
        bindings.pop(artifact)
    elif change == "missing_file":
        (tmp_path / bindings[artifact]).unlink()
    else:
        (tmp_path / bindings[artifact]).write_bytes(b"wrong")
    result = verify_lineage_files(workload, bindings, tmp_path)
    assert not result["all_files_verified"]
    assert len(result["files"]) == 8
    assert sum(r["status"] == "verified" for r in result["files"]) == 7


def test_unknown_binding_is_not_silently_ignored(tmp_path):
    workload, bindings = fixture(tmp_path)
    bindings["typo"] = "output"
    with pytest.raises(ContractError, match="unknown file binding"):
        verify_lineage_files(workload, bindings, tmp_path)


@pytest.mark.parametrize("path", ["../outside", "/outside"])
def test_escape_rejected(tmp_path, path):
    with pytest.raises(ContractError, match="escapes"):
        verify_file(tmp_path, path, "a" * 64)


@pytest.mark.skipif(sys.platform == "win32", reason="POSIX FIFO")
def test_fifo_does_not_block(tmp_path):
    os.mkfifo(tmp_path / "pipe")
    assert verify_file(tmp_path, "pipe", "a" * 64)["reason"] == "not_regular_file"


@pytest.mark.parametrize("mutation", ["edit", "replace"])
def test_file_changed_while_hashing_is_unknown(tmp_path, monkeypatch, mutation):
    path = tmp_path / "file"
    path.write_bytes(b"before")
    original = hashlib.file_digest

    def changed(stream, algorithm):
        digest = original(stream, algorithm)
        if mutation == "replace":
            path.unlink()
        path.write_bytes(b"after")
        return digest

    monkeypatch.setattr(hashlib, "file_digest", changed)
    result = verify_file(tmp_path, "file", hashlib.sha256(b"before").hexdigest())
    assert result["status"] == "unknown"
    assert result["reason"] == "file_changed_during_hash"
    assert result["sha256"] is None


def test_script_exit_and_report_bind_to_inputs(tmp_path):
    workload, bindings = fixture(tmp_path)
    source, files, out = (
        tmp_path / name for name in ("workload.json", "bindings.json", "result.json")
    )
    source.write_text(json.dumps(workload))
    files.write_text(json.dumps(bindings))
    script = Path(__file__).resolve().parents[2] / "scripts/verify_lineage_files.py"
    args = [
        sys.executable,
        str(script),
        "--workload",
        str(source),
        "--bindings",
        str(files),
        "--root",
        str(tmp_path),
        "--out",
        str(out),
    ]
    completed = subprocess.run(args, capture_output=True, text=True)
    assert completed.returncode == 0, completed.stderr
    assert json.loads(out.read_text())["all_files_verified"]
    before = out.read_bytes()
    assert subprocess.run(args, capture_output=True).returncode != 0
    assert out.read_bytes() == before
    out.unlink()
    files.write_text("{}")
    assert subprocess.run(args, capture_output=True).returncode == 3
    report = json.loads(out.read_text())
    assert not report["all_files_verified"]
    assert len(report["files"]) == 8


def test_directory_is_rejected_without_descriptor_leak(tmp_path):
    def descriptors():
        if sys.platform == "win32":
            import psutil

            return psutil.Process().num_handles()
        directory = "/dev/fd" if sys.platform == "darwin" else "/proc/self/fd"
        return set(os.listdir(directory))

    before = descriptors()
    for _ in range(10):
        assert verify_file(tmp_path, ".", "a" * 64)["reason"] == "not_regular_file"
    assert descriptors() == before
