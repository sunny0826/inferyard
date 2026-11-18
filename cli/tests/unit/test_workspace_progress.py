"""Read-only progress fixtures, including authoritative sealed run/report evidence."""

import hashlib
import json
import shlex
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.provisioning.preparation_io import json_bytes
from inferyard.analysis.ledger import read_trial
from inferyard.reporting.studio_report import write_studio_report
from tests.helpers import fixture_run


def invoke(capsys, workspace):
    code = main(["progress", "--workspace", str(workspace)])
    output = capsys.readouterr()
    return code, json.loads(output.out), output.err


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(json_bytes(value))


@pytest.fixture
def workspace(tmp_path, capsys):
    path = tmp_path / "中文 workspace"
    assert main(["init", "--out", str(path)]) == 0
    capsys.readouterr()
    return path


def preflight(workspace):
    path = workspace / "preflight/device-1/device-preflight.json"
    save(
        path,
        {
            "kind": "device_preflight.v1",
            "hardware": {"platform": "Darwin"},
            "recommendation": {"status": "recommended"},
        },
    )
    return path


def candidate(workspace, name="zh-core"):
    path = workspace / "candidates" / (name + "-1")
    save(
        path / "preparation.json",
        {
            "schema_version": 3,
            "kind": "config_candidate.v1",
            "bundle": str(workspace / "bundles" / (name + ".json")),
            "candidate": str(path / "candidate.toml"),
        },
    )
    (path / "candidate.toml").write_text("fixture, never executed")
    return path


def binding(workspace, candidate):
    path = workspace / "bound" / candidate.name
    save(
        path / "preparation.json",
        {
            "schema_version": 3,
            "kind": "config_binding.v1",
            "candidate": str(candidate / "candidate.toml"),
            "config": str(path / "config.toml"),
        },
    )
    (path / "config.toml").write_text("fixture, never executed")
    return path


def snapshot(workspace):
    return {
        str(p): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in workspace.rglob("*")
        if p.is_file()
    }


def fixture_bundle_run(workspace, name, states):
    root = fixture_run(workspace / "results" / name / "runs", states=states)
    bundle = read_trial(root)["bundle"]
    save(workspace / "bundles" / (name + ".json"), bundle)
    return root


def test_two_runs_report_and_present_default_scope(workspace, capsys):
    preflight(workspace)
    roots = []
    for name in ("zh-core", "zh-svg-pelican"):
        binding(workspace, candidate(workspace, name))
        states = ["completed"] if name == "zh-core" else ["completed", "completed"]
        roots.append(fixture_bundle_run(workspace, name, states))
    old = workspace / "reports/summary-legacy"
    old.mkdir(parents=True)
    (old / "index.json").write_text('{"report_format_version": 8}')
    code, result, _ = invoke(capsys, workspace)
    assert code == 0 and all(b["phase"] == "run_complete" for b in result["bundles"])
    assert result["next_action"] == "report" and not result["default_scope_complete"]
    assert result["report"] == {"status": "missing", "path": None, "runs": []}
    arguments = shlex.split(result["next_command"])
    assert arguments[:4] == ["inferyard", "report", "--runs", str(roots[0])]
    assert Path(arguments[-1]).name == "summary-1"
    report = workspace / "reports/summary-1"
    write_studio_report(roots, report)
    before = snapshot(workspace)
    code, result, _ = invoke(capsys, workspace)
    assert code == 0 and result["report"]["status"] == "present"
    assert result["report"]["path"] == str(report)
    assert set(result["report"]["runs"]) == {str(root) for root in roots}
    assert result["next_action"] == "done" and result["next_command"] is None
    assert result["default_scope_complete"] is True
    assert snapshot(workspace) == before
