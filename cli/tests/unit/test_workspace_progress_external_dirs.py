"""Tests for progress with sibling results and reports directories outside workspace."""

from __future__ import annotations

import json
import shlex
from pathlib import Path
from types import SimpleNamespace

from inferyard.application.workspace_progress import inspect_workspace
from inferyard.cli import main
from inferyard.config.community_workspace import initialize


def _write_json(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload), encoding="utf-8")


def test_progress_external_results_in_next_command_and_cli_flags(
    tmp_path: Path, capsys
) -> None:
    base = tmp_path / ".inferyard"
    base.mkdir()
    workspace = base / "workspace"
    results = base / "results"
    reports = base / "reports"
    initialize(SimpleNamespace(out=workspace, community_bundle=None))
    _write_json(
        workspace / "preflight" / "device-1" / "device-preflight.json",
        {
            "kind": "device_preflight.v1",
            "schema_version": 3,
            "hardware": {"platform": "Darwin"},
            "recommendation": {"status": "recommended"},
        },
    )

    state = inspect_workspace(workspace, results_root=results, reports_root=reports)
    core = next(b for b in state["bundles"] if b["bundle"] == "zh-core")
    assert core["next_action"] == "prepare_candidate"
    tokens = shlex.split(core["next_command"])
    assert "--results" in tokens
    assert Path(tokens[tokens.index("--results") + 1]).resolve() == results.resolve()

    code = main(
        [
            "progress",
            "--workspace",
            str(workspace),
            "--results",
            str(results),
            "--reports",
            str(reports),
        ]
    )
    assert code == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["kind"] == "progress"
    assert payload["progress_version"] == 2
    cli_core = next(b for b in payload["bundles"] if b["bundle"] == "zh-core")
    cli_tokens = shlex.split(cli_core["next_command"])
    assert "--results" in cli_tokens
    assert Path(cli_tokens[cli_tokens.index("--results") + 1]).resolve() == results.resolve()


def test_progress_reads_runs_and_suggests_report_in_external_roots(
    tmp_path: Path, monkeypatch
) -> None:
    base = tmp_path / ".inferyard"
    base.mkdir()
    workspace = base / "workspace"
    results = base / "results"
    reports = base / "reports"
    initialize(SimpleNamespace(out=workspace, community_bundle="zh-core"))
    _write_json(
        workspace / "preflight" / "device-1" / "device-preflight.json",
        {
            "kind": "device_preflight.v1",
            "schema_version": 3,
            "hardware": {"platform": "Darwin"},
            "recommendation": {"status": "recommended"},
        },
    )
    bundle_path = workspace / "bundles" / "zh-core.json"
    bundle = json.loads(bundle_path.read_text(encoding="utf-8"))

    cand_dir = workspace / "candidates" / "zh-core-1"
    cand_file = cand_dir / "candidate.toml"
    cand_dir.mkdir(parents=True)
    cand_file.write_text("# cand\n", encoding="utf-8")
    _write_json(
        cand_dir / "preparation.json",
        {
            "kind": "config_candidate.v1",
            "schema_version": 3,
            "candidate": str(cand_file),
            "bundle": str(bundle_path),
        },
    )
    bound_dir = workspace / "bound" / "zh-core-1"
    bound_cfg = bound_dir / "config.toml"
    bound_dir.mkdir(parents=True)
    bound_cfg.write_text("# bound\n", encoding="utf-8")
    _write_json(
        bound_dir / "preparation.json",
        {
            "kind": "config_binding.v1",
            "schema_version": 3,
            "candidate": str(cand_file),
            "config": str(bound_cfg),
        },
    )

    run_dir = results / "20261124T000000Z-run1"
    _write_json(run_dir / "inputs.json", {})

    def fake_read_trial(path: Path, *, metadata: dict) -> dict:
        assert path.resolve() == run_dir.resolve()
        metadata["manifest"] = {"sealed": True}
        return {
            "bundle": bundle,
            "config": {"bundle": {"path": str(bundle_path.resolve())}},
            "run": {"diagnostic": False, "plan_sha256": "a" * 64, "trial_id": "t1"},
            "plan": {"trials": [{"trial_id": "t1"}]},
            "summary": {"completeness": "complete"},
            "requests": [],
        }

    def fake_plan_progress(plan, runs):
        return [{"status": "complete"}]

    monkeypatch.setattr("inferyard.analysis.ledger.read_trial", fake_read_trial)
    monkeypatch.setattr("inferyard.analysis.ledger.plan_progress", fake_plan_progress)

    state = inspect_workspace(workspace, results_root=results, reports_root=reports)
    assert state["bundles"][0]["phase"] == "run_complete"
    assert state["next_action"] == "report"
    tokens = shlex.split(state["next_command"])
    assert Path(tokens[tokens.index("--out") + 1]).resolve() == (reports / "summary-1").resolve()

    # A second workspace sharing the same external results directory must not
    # inherit workspace A's completed run before running its own bound config.
    workspace_b = base / "workspace-b"
    initialize(SimpleNamespace(out=workspace_b, community_bundle="zh-core"))
    _write_json(
        workspace_b / "preflight" / "device-1" / "device-preflight.json",
        {
            "kind": "device_preflight.v1",
            "schema_version": 3,
            "hardware": {"platform": "Darwin"},
            "recommendation": {"status": "recommended"},
        },
    )
    bundle_b = workspace_b / "bundles" / "zh-core.json"
    cand_b = workspace_b / "candidates" / "zh-core-1"
    cand_b_file = cand_b / "candidate.toml"
    cand_b.mkdir(parents=True)
    cand_b_file.write_text("# cand b\n", encoding="utf-8")
    _write_json(
        cand_b / "preparation.json",
        {
            "kind": "config_candidate.v1",
            "schema_version": 3,
            "candidate": str(cand_b_file),
            "bundle": str(bundle_b),
        },
    )
    bound_b = workspace_b / "bound" / "zh-core-1"
    bound_b_cfg = bound_b / "config.toml"
    bound_b.mkdir(parents=True)
    bound_b_cfg.write_text("# bound b\n", encoding="utf-8")
    _write_json(
        bound_b / "preparation.json",
        {
            "kind": "config_binding.v1",
            "schema_version": 3,
            "candidate": str(cand_b_file),
            "config": str(bound_b_cfg),
        },
    )

    state_b = inspect_workspace(workspace_b, results_root=results, reports_root=reports)
    assert state_b["bundles"][0]["phase"] == "run_missing"
    assert state_b["next_action"] == "run"
    assert state_b["bundles"][0]["runs"] == []
