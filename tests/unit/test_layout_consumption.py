"""Remaining offline scripts consume actual old/new run evidence read-only."""

import importlib
import sys
from pathlib import Path

import pytest

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json, sha256_file
from tests.evidence_layout_helpers import seal_layout
from tests.unit.test_external_measurement_import import channel
from tests.unit.test_layout_surfaces import sources as layout_sources
from tests.unit.test_layout_surfaces import tree_bytes

sources = layout_sources


@pytest.fixture
def scripts(monkeypatch):
    monkeypatch.syspath_prepend(str(Path(__file__).resolve().parents[2] / "scripts"))
    return importlib.import_module("import_external_measurements"), importlib.import_module(
        "benchmark_cost_worker"
    )


def import_spec(root):
    data = read_trial(root)
    rows = [r for r in data["requests"] if type(r["t_send_ns"]) is int]
    start, end = min(r["t_send_ns"] for r in rows), max(r["t_terminal_ns"] for r in rows)
    measurement = channel()
    measurement["source"]["clock_id"] = rows[0]["clock_id"]
    measurement["max_gap_ns"] = end - start
    for sample, tick in zip(measurement["samples"], (start, end), strict=True):
        sample["monotonic_ns"] = tick
    return {
        "definition": "external_measurements.v1",
        "run_id": data["run"]["run_id"],
        "manifest_sha256": sha256_file(root / "manifest.json"),
        "window_ns": [start, end],
        "channels": [measurement],
    }


@pytest.mark.parametrize("layout", [1, 2])
def test_external_import_replays_actual_source_without_qualification(
    sources, scripts, tmp_path, monkeypatch, capsys, layout
):
    root = sources[layout - 1]
    importer, _ = scripts
    before = tree_bytes(root)
    spec_path, out = tmp_path / "measurement.json", tmp_path / "import"
    spec_path.write_bytes(json_bytes(import_spec(root)))
    monkeypatch.setattr(
        sys, "argv", ["import", "--spec", str(spec_path), "--run", str(root), "--out", str(out)]
    )
    importer.main()
    assert importer.verify_import(out) == {
        "verified": True,
        "model_requests": 0,
        "physical_measurement_qualification": False,
    }
    result = read_json(out / "analysis.json")
    assert result["planned_requests"] == len(read_trial(root)["requests"])
    assert not result["physical_measurement_qualification"]
    assert tree_bytes(root) == before
    assert "software_import_verified" in capsys.readouterr().out


def test_external_import_does_not_trust_resealed_bad_member(
    sources, scripts, tmp_path, monkeypatch
):
    root = sources[1]
    importer, _ = scripts
    spec = import_spec(root)
    inputs = read_json(root / "inputs.json")
    inputs["documents"]["config"]["sha256"] = "0" * 64
    (root / "inputs.json").write_bytes(json_bytes(inputs))
    seal_layout(root, inputs["documents"]["run"]["payload"])
    spec["manifest_sha256"] = sha256_file(root / "manifest.json")
    spec_path, out = tmp_path / "measurement.json", tmp_path / "import"
    spec_path.write_bytes(json_bytes(spec))
    monkeypatch.setattr(
        sys, "argv", ["import", "--spec", str(spec_path), "--run", str(root), "--out", str(out)]
    )
    with pytest.raises(EvidenceError, match="invalid_inputs_container"):
        importer.main()
    assert not out.exists()


@pytest.mark.parametrize("layout", [1, 2])
@pytest.mark.parametrize("operation", ["read_trial", "report"])
def test_cost_worker_consumes_actual_layout_readonly(sources, scripts, tmp_path, layout, operation):
    root = sources[layout - 1]
    _, worker = scripts
    before = tree_bytes(root)
    corpus, out = tmp_path / "corpus", tmp_path / "worker"
    corpus.mkdir()
    out.mkdir()
    (corpus / "corpus.json").write_bytes(json_bytes({"runs": [str(root)]}))
    assert worker.workload(operation, corpus, out) == 0
    result = read_json(out / "result.json")
    if operation == "read_trial":
        assert result[0]["summary"]["counts"]["planned"] == len(read_trial(root)["requests"])
    else:
        assert result["report_format_version"] == 8
        assert len(result["runs"][0]["requests"]) == len(read_trial(root)["requests"])
    assert tree_bytes(root) == before
