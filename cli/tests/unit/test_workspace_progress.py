"""Read-only progress fixtures, including authoritative sealed run/report evidence."""

import hashlib
import json
import shlex
from pathlib import Path

import pytest

from inferyard.cli import main
from inferyard.config.preparation_io import json_bytes
from inferyard.evidence.ledger import read_trial
from inferyard.reporting.report import write_report
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


def test_init_layout_roadmap_and_progress_read_only(workspace, capsys):
    for name in ("preflight", "candidates", "bound", "reports"):
        assert (workspace / name).is_dir()
        assert not list((workspace / name).iterdir())
    roadmap = (workspace / "README.md").read_text()
    assert "{{WORKSPACE}}" not in roadmap and str(workspace) in roadmap
    assert "progress --workspace" in roadmap
    before = snapshot(workspace)
    code, result, error = invoke(capsys, workspace)
    assert code == 0 and not error
    assert set(result) == {
        "kind",
        "progress_version",
        "workspace",
        "device_preflight",
        "bundles",
        "report",
        "next_action",
        "next_command",
        "default_scope_complete",
    }
    assert result["kind"] == "progress" and result["progress_version"] == 1
    assert [b["bundle"] for b in result["bundles"]] == ["zh-core", "zh-svg-pelican"]
    assert all(b["phase"] == "preflight_missing" for b in result["bundles"])
    assert result["next_action"] == "device_check"
    args = shlex.split(result["next_command"])
    assert args[:3] == ["inferyard", "device-check", "--json"]
    assert Path(args[-1]).is_absolute() and not Path(args[-1]).exists()
    assert result["default_scope_complete"] is False
    assert snapshot(workspace) == before


@pytest.mark.parametrize(
    ("stage", "phase", "action"),
    [
        ("preflight", "candidate_missing", "prepare_candidate"),
        ("candidate", "bound_missing", "bind"),
        ("bound", "run_missing", "run"),
    ],
)
def test_preparation_phases(workspace, capsys, stage, phase, action):
    preflight(workspace)
    if stage != "preflight":
        prepared = candidate(workspace)
        if stage == "bound":
            binding(workspace, prepared)
    code, result, _ = invoke(capsys, workspace)
    core = result["bundles"][0]
    assert code == 0 and core["phase"] == phase and core["next_action"] == action
    assert result["next_action"] == action
    assert str(workspace) in core["next_command"]
    assert core["blocked_reason"] is None


def fixture_bundle_run(workspace, name, states):
    root = fixture_run(workspace / "results" / name / "runs", states=states)
    bundle = read_trial(root)["bundle"]
    save(workspace / "bundles" / (name + ".json"), bundle)
    return root


def test_incomplete_and_unsealed_run_are_not_complete(workspace, capsys):
    preflight(workspace)
    prepared = candidate(workspace)
    bound = binding(workspace, prepared)
    root = fixture_bundle_run(workspace, "zh-core", ["completed", "not_executed"])
    code, result, _ = invoke(capsys, workspace)
    core = result["bundles"][0]
    assert code == 0 and core["phase"] == "run_incomplete"
    # Single-run layout has no batch root, so progress points back at the bound config
    # instead of a resume that cannot find a batch.
    assert core["next_action"] == "run" and result["next_action"] == "run"
    assert str(bound / "config.toml") in core["next_command"]
    assert core["blocked_reason"] == "single_run_resume_not_supported"
    assert core["runs"] == [{"path": str(root), "complete": False, "sealed": True}]
    (root / "manifest.json").unlink()
    code, result, _ = invoke(capsys, workspace)
    assert code == 0 and result["bundles"][0]["runs"][0]["sealed"] is False
    assert not result["default_scope_complete"]


def test_two_runs_report_and_verified_default_scope(workspace, capsys):
    preflight(workspace)
    roots = []
    for name in ("zh-core", "zh-svg-pelican"):
        binding(workspace, candidate(workspace, name))
        states = ["completed"] if name == "zh-core" else ["completed", "completed"]
        roots.append(fixture_bundle_run(workspace, name, states))
    code, result, _ = invoke(capsys, workspace)
    assert code == 0 and all(b["phase"] == "run_complete" for b in result["bundles"])
    assert result["next_action"] == "report" and not result["default_scope_complete"]
    report = workspace / "reports/summary-1"
    write_report(roots, report)
    before = snapshot(workspace)
    code, result, _ = invoke(capsys, workspace)
    assert code == 0 and result["report"]["status"] == "verified"
    assert set(result["report"]["runs"]) == {str(root) for root in roots}
    assert result["next_action"] == "done" and result["next_command"] is None
    assert result["default_scope_complete"] is True
    assert snapshot(workspace) == before
    (report / "artifact-manifest.json").unlink()
    code, result, _ = invoke(capsys, workspace)
    assert code == 0 and result["report"]["status"] == "present"
    assert result["next_action"] == "verify" and not result["default_scope_complete"]


@pytest.mark.parametrize(
    "damage",
    [
        "missing",
        "no_preparation",
        "invalid_json",
        "wrong_kind",
        "bad_candidate",
        "bad_run",
        "bad_preflight",
        "empty_scope",
        "duplicate_scope",
        "invalid_name",
    ],
)
def test_unreadable_artifacts_always_exit_two(workspace, capsys, damage):
    if damage == "missing":
        workspace = workspace / "missing"
    elif damage == "no_preparation":
        (workspace / "preparation.json").unlink()
    elif damage == "invalid_json":
        (workspace / "preparation.json").write_text('{"kind":')
    elif damage == "wrong_kind":
        save(workspace / "preparation.json", {"kind": "other", "schema_version": 3})
    elif damage == "bad_candidate":
        candidate(workspace).joinpath("candidate.toml").unlink()
    elif damage == "bad_run":
        save(workspace / "results/broken/inputs.json", {})
    elif damage in ("empty_scope", "duplicate_scope", "invalid_name"):
        path = workspace / "preparation.json"
        value = json.loads(path.read_text())
        if damage == "empty_scope":
            value["bundles"] = []
        elif damage == "duplicate_scope":
            value["bundles"].append(value["bundles"][0])
        else:
            value["bundles"][0]["bundle"] = "../outside"
        save(path, value)
    else:
        preflight(workspace).write_text('{"kind":"device_preflight.v1","kind":"duplicate"}')
    code, result, error = invoke(capsys, workspace)
    assert code == 2 and result["status"] == "blocked" and error


def test_corrupt_report_is_rejected_even_before_runs(workspace, capsys):
    save(workspace / "reports/broken/index.json", {"runs": []})
    code, result, error = invoke(capsys, workspace)
    assert code == 2 and result["status"] == "blocked" and error


def test_unsealed_complete_run_stays_incomplete(workspace, capsys):
    preflight(workspace)
    binding(workspace, candidate(workspace))
    root = fixture_bundle_run(workspace, "zh-core", ["completed"])
    (root / "manifest.json").unlink()
    code, result, _ = invoke(capsys, workspace)
    assert code == 0 and result["bundles"][0]["phase"] == "run_incomplete"
    assert result["bundles"][0]["runs"] == [{"path": str(root), "complete": False, "sealed": False}]


def test_blocked_preflight_remains_readable(workspace, capsys):
    path = preflight(workspace)
    value = json.loads(path.read_text())
    value["recommendation"] = {"status": "blocked", "reason": "no_local_model"}
    save(path, value)
    code, result, _ = invoke(capsys, workspace)
    assert code == 0 and result["device_preflight"]["status"] == "ok"
    assert result["bundles"][0]["blocked_reason"] == "no_local_model"


def test_new_command_never_reuses_existing_output(workspace, capsys):
    (workspace / "preflight/device-1").mkdir()
    code, result, _ = invoke(capsys, workspace)
    assert code == 0
    assert shlex.split(result["next_command"])[-1] == str(workspace / "preflight/device-2")
