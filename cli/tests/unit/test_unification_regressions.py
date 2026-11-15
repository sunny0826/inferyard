"""Regressions found while unifying the single and experiment read/write paths."""

import json

import pytest

from inferyard.application.types import CommandRequest
from inferyard.errors import PreflightError
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import sha256_file
from inferyard.reporting.studio_report import write_studio_report
from inferyard.runtime.runner import load_rerun
from tests.helpers import fixture_run


def test_studio_report_omits_prompt_answer_and_scored_content(tmp_path):
    root = fixture_run(tmp_path / "runs")
    out = tmp_path / "report"
    document = write_studio_report([root], out)
    raw = (out / "studio.json").read_text()
    assert "<script>alert(1)</script>" not in raw
    assert "reference_answer" not in raw and '"content"' not in raw
    run_ids = [run["source"]["run_id"] for run in document["runs"]]
    assert run_ids == [root.name.removeprefix("run-")] or len(run_ids) == 1
    assert [row["case_id"] for row in document["runs"][0]["requests"]]


def test_experiment_parent_cannot_be_reinterpreted_as_single_rerun(tmp_path, monkeypatch):
    root = fixture_run(tmp_path / "runs")
    run = json.loads((root / "run.json").read_text())
    run["execution_mode"] = "experiment"
    (root / "run.json").write_bytes(json_bytes(run))
    manifest = json.loads((root / "manifest.json").read_text())
    manifest["execution_mode"] = "experiment"
    manifest["files"]["run.json"].update(
        sha256=sha256_file(root / "run.json"), bytes=(root / "run.json").stat().st_size
    )
    (root / "manifest.json").write_bytes(json_bytes(manifest))

    def process_must_not_be_queried(*args):
        pytest.fail("incompatible parent must be rejected before process lookup")

    monkeypatch.setattr("inferyard.runtime.runner.process_start_ticks", process_must_not_be_queried)
    with pytest.raises(PreflightError, match="rerun_requires_single_run_source"):
        load_rerun(
            CommandRequest(
                "run", from_run=root, server_pid=123, endpoint_url="http://127.0.0.1:9090"
            )
        )
