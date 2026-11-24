"""Regression tests for workspace progress with local-engine artifacts."""

from __future__ import annotations

import json
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.application.workspace_progress import inspect_workspace
from inferyard.config.community_workspace import initialize
from inferyard.provisioning.preparation_io import PreparationError


def _init_workspace(workspace: Path) -> None:
    initialize(SimpleNamespace(out=workspace, community_bundle=None))


def _write_json(path: Path, payload: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload), encoding="utf-8")


def _write_preflight(workspace: Path) -> None:
    _write_json(
        workspace / "preflight" / "device-1" / "device-preflight.json",
        {
            "kind": "device_preflight.v1",
            "schema_version": 3,
            "hardware": {"platform": "Darwin"},
            "recommendation": {"status": "recommended"},
        },
    )


def test_progress_reads_local_engine_candidate_and_binding(tmp_path: Path) -> None:
    workspace = tmp_path / "ws"
    _init_workspace(workspace)
    _write_preflight(workspace)

    cand_dir = workspace / "candidates" / "zh-core-1"
    cand_file = cand_dir / "candidate.toml"
    cand_dir.mkdir(parents=True)
    cand_file.write_text("# local candidate\n", encoding="utf-8")
    _write_json(
        cand_dir / "preparation.json",
        {
            "kind": "local-engine-candidate.v1",
            "schema_version": 3,
            "out": str(cand_dir),
            "model_requests_sent": 0,
            "ready_to_run": False,
            "candidate": str(cand_file),
            "installation": str(cand_dir / "installation.json"),
            "family": "polystrata",
            "model": "/models/qwen.gguf",
            "bundle": str(workspace / "bundles" / "zh-core.json"),
        },
    )

    state = inspect_workspace(workspace)
    core = next(b for b in state["bundles"] if b["bundle"] == "zh-core")
    assert core["phase"] == "bound_missing"
    assert core["next_action"] == "bind"
    assert Path(core["candidate_path"]).resolve() == cand_dir.resolve()

    bound_dir = workspace / "bound" / "zh-core-1"
    bound_cfg = bound_dir / "config.toml"
    bound_dir.mkdir(parents=True)
    bound_cfg.write_text("# local bound\n", encoding="utf-8")
    _write_json(
        bound_dir / "preparation.json",
        {
            "kind": "local-engine-binding-output.v1",
            "schema_version": 3,
            "out": str(bound_dir),
            "model_requests_sent": 0,
            "ready_to_run": False,
            "config": str(bound_cfg),
            "candidate": str(cand_file),
            "pid": 12345,
            "start_ticks": 99999,
            "endpoint": "http://127.0.0.1:48857",
            "binding": str(bound_dir / "binding.json"),
        },
    )

    bound_state = inspect_workspace(workspace)
    core_bound = next(b for b in bound_state["bundles"] if b["bundle"] == "zh-core")
    assert core_bound["phase"] == "run_missing"
    assert core_bound["next_action"] == "run"
    assert Path(core_bound["bound_path"]).resolve() == bound_dir.resolve()


@pytest.mark.parametrize(
    ("subdir", "payload"),
    [
        (
            "candidates/zh-core-1",
            {
                "kind": "unknown-candidate.v1",
                "schema_version": 3,
                "candidate": "x",
                "bundle": "y",
            },
        ),
        (
            "candidates/zh-core-1",
            {
                "kind": "local-engine-candidate.v1",
                "schema_version": 3,
                "candidate": "",
                "bundle": "y",
            },
        ),
        (
            "bound/zh-core-1",
            {
                "kind": "local-engine-binding-output.v1",
                "schema_version": 3,
                "candidate": "x",
                "config": None,
            },
        ),
    ],
)
def test_progress_rejects_malformed_preparation_records(
    tmp_path: Path, subdir: str, payload: dict
) -> None:
    workspace = tmp_path / "ws"
    _init_workspace(workspace)
    _write_preflight(workspace)
    _write_json(workspace / subdir / "preparation.json", payload)

    with pytest.raises(PreparationError, match="workspace_artifact_unreadable"):
        inspect_workspace(workspace)
