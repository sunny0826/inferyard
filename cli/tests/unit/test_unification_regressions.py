"""Regressions found while unifying the single and experiment read/write paths."""

import json
from pathlib import Path

import pytest

from inferyard.application.types import CommandRequest
from inferyard.evidence.storage import json_bytes, sha256_file
from inferyard.platforms.identity import PreflightError
from inferyard.reporting.report import write_report
from inferyard.reporting.report_common import evidence_url
from inferyard.runtime.runner import load_rerun
from tests.helpers import fixture_run


def test_report_local_links_support_cross_drive_outputs(tmp_path, monkeypatch):
    source = tmp_path / "source with spaces" / "manifest.json"
    out = tmp_path / "report"
    assert evidence_url(source, out) == "../source%20with%20spaces/manifest.json"

    def different_drive(*args):
        raise ValueError("path is on mount D:, start on mount C:")

    monkeypatch.setattr("inferyard.reporting.report_common.os.path.relpath", different_drive)
    assert evidence_url(source, out) == source.resolve().as_uri()
    root = fixture_run(tmp_path / "runs")
    index = write_report([root], out)
    assert index["runs"][0]["evidence"]["manifest.json"] == (root / "manifest.json").as_uri()


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


def test_report_preserves_escaped_prompt_as_well_as_answer(tmp_path):
    root = fixture_run(tmp_path / "runs")
    out = tmp_path / "report"
    index = write_report([root], out)
    prompt = index["contents"][index["runs"][0]["requests"][0]["prompt_ref"]]
    assert "<script>" in prompt
    html = Path(out / "report.html").read_text()
    assert "<script>alert" not in html
    assert "&lt;script&gt;alert(1)&lt;/script&gt;" in html
