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


def test_context_speed_preset_plan_and_batch_scoped_to_workspace(
    tmp_path: Path, capsys, monkeypatch
) -> None:
    import hashlib
    import shutil

    from inferyard.config.toml_writer import render
    from inferyard.contracts.context_speed_profile import GENERATION

    base = tmp_path / ".inferyard"
    base.mkdir()
    results = base / "results"
    results.mkdir()
    reports = base / "reports"

    model_file = tmp_path / "model.gguf"
    model_file.write_bytes(b"gguf")
    tmpl_file = tmp_path / "template.jinja"
    tmpl_file.write_text("{{ prompt }}", encoding="utf-8")
    bin_file = tmp_path / "llama-server"
    bin_file.write_bytes(b"bin")
    sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()  # noqa: E731

    def setup_speed_workspace(ws: Path) -> Path:
        initialize(SimpleNamespace(out=ws, community_bundle="zh-context-speed"))
        _write_json(
            ws / "preflight" / "device-1" / "device-preflight.json",
            {
                "kind": "device_preflight.v1",
                "schema_version": 3,
                "hardware": {"platform": "Darwin"},
                "recommendation": {"status": "recommended"},
            },
        )
        bundle_path = ws / "bundles" / "zh-context-speed.json"
        bundle = json.loads(bundle_path.read_text(encoding="utf-8"))
        cand_dir = ws / "candidates" / "zh-context-speed-1"
        cand_dir.mkdir(parents=True)
        cand_file = cand_dir / "candidate.toml"
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
        bound_dir = ws / "bound" / "zh-context-speed-1"
        bound_dir.mkdir(parents=True)
        bound_cfg = bound_dir / "config.toml"
        manifest_file = tmp_path / "engine-sha256.json"
        _write_json(manifest_file, {bin_file.name: sha(bin_file)})
        config_doc = {
            "schema_version": 3,
            "device": {"id": "test-host", "client_position": "same_host_native"},
            "model": {
                "display_name": "test-model",
                "repo": "local",
                "revision": "rev1",
                "local_path": str(model_file),
                "sha256": sha(model_file),
                "packing": "GGUF",
                "template_path": str(tmpl_file),
                "template_sha256": sha(tmpl_file),
            },
            "engine": {
                "adapter": "prism_llama_server_v1",
                "release": "prism-b1",
                "backend": "metal",
                "binary_path": str(bin_file),
                "binary_sha256": sha(bin_file),
                "runtime_library_manifest": str(manifest_file),
                "startup_args": [
                    "-m",
                    str(model_file),
                    "-c",
                    "65536",
                    "-np",
                    "1",
                    "--no-cache-prompt",
                    "--no-cache-idle-slots",
                    "--cache-ram",
                    "0",
                    "--reasoning",
                    "off",
                ],
            },
            "endpoint": {
                "url": "http://127.0.0.1:18080",
                "server_pid": 12345,
                "process_start_ticks": 99999,
            },
            "bundle": {"path": str(bundle_path.resolve()), "version": bundle["version"]},
            "generation": dict(GENERATION),
            "conditions": {
                "context_size": 65536,
                "threads": 4,
                "threads_batch": 4,
                "slots": 1,
                "cache_policy": "disabled",
                "ac_online": True,
                "profile": "unknown",
                "governor": "unknown",
                "epp": "unknown",
                "allow_unknown_environment": True,
                "model_loaded": True,
                "background_load": "idle",
            },
            "execution": {"warmup_count": 1, "timeout_seconds": 3},
            "output": {
                "root": str(results),
                "min_available_memory_bytes": 10 * 1024**3,
                "min_disk_bytes": 10 * 1024**3,
            },
        }
        bound_cfg.write_text(render(config_doc), encoding="utf-8")
        _write_json(
            bound_dir / "preparation.json",
            {
                "kind": "config_binding.v1",
                "schema_version": 3,
                "candidate": str(cand_file),
                "config": str(bound_cfg),
            },
        )
        return bound_cfg

    ws_a = base / "workspace-a"
    ws_b = base / "workspace-b"
    cfg_a = setup_speed_workspace(ws_a)
    setup_speed_workspace(ws_b)

    plan_dir = results / "speed-plan-1"
    code = main(
        [
            "plan",
            "--preset",
            "context-speed",
            "--config",
            str(cfg_a),
            "--out",
            str(plan_dir),
        ]
    )
    assert code == 0
    capsys.readouterr()

    plan_doc = json.loads((plan_dir / "plan.json").read_text(encoding="utf-8"))
    assert (
        Path(plan_doc["runtime_bindings"][0]["source_directory"]).resolve()
        == cfg_a.parent.resolve()
    )

    state_a = inspect_workspace(ws_a, results_root=results, reports_root=reports)
    assert state_a["bundles"][0]["phase"] == "run_missing"
    assert state_a["bundles"][0]["next_action"] == "run"

    state_b = inspect_workspace(ws_b, results_root=results, reports_root=reports)
    assert state_b["bundles"][0]["phase"] == "plan_missing"
    assert state_b["bundles"][0]["next_action"] == "freeze_plan"

    # Simulate a completed batch using workspace A's frozen plan in the shared results dir.
    batch_dir = results / "speed-batch-1"
    shutil.copytree(plan_dir, batch_dir / "frozen-plan")
    _write_json(batch_dir / "batch.json", {"kind": "batch"})
    bundle_a = json.loads((ws_a / "bundles" / "zh-context-speed.json").read_text(encoding="utf-8"))
    fake_loaded = {
        "context-speed": SimpleNamespace(
            bundle=SimpleNamespace(to_dict=lambda: bundle_a),
        )
    }
    monkeypatch.setattr(
        "inferyard.application.context_speed_progress.read_inputs",
        lambda path: (None, fake_loaded, None),
    )

    batch_a = inspect_workspace(ws_a, results_root=results, reports_root=reports)
    assert batch_a["bundles"][0]["phase"] == "results_available"
    assert batch_a["bundles"][0]["next_action"] == "speed_results"

    batch_b = inspect_workspace(ws_b, results_root=results, reports_root=reports)
    assert batch_b["bundles"][0]["phase"] == "plan_missing"
    assert batch_b["bundles"][0]["next_action"] == "freeze_plan"

    # An active batch with an unsealed trial in runs/ stays in run_missing rather
    # than failing workspace progress with workspace_artifact_unreadable.
    from inferyard.analysis.scoring import ScoringContext
    from inferyard.config.plan_inputs import read_frozen_plan
    from inferyard.evidence.journal import TrialJournal

    live_batch = results / "speed-batch-2"
    shutil.copytree(plan_dir, live_batch / "frozen-plan")
    _write_json(live_batch / "batch.json", {"kind": "batch"})
    frozen_plan, frozen_loaded = read_frozen_plan(live_batch / "frozen-plan" / "plan.json")
    first_loaded = next(iter(frozen_loaded.values()))
    live_store = TrialJournal(
        live_batch / "runs",
        frozen_plan,
        frozen_plan["trials"][0]["trial_id"],
        first_loaded.config.to_dict(),
        first_loaded.bundle.to_dict(),
        scoring_context=ScoringContext(),
    )
    for stream in live_store._logs.values():
        stream.close()

    def fail_if_called(path):
        raise AssertionError(f"read_inputs should not run on unsealed batch {path}")

    monkeypatch.setattr("inferyard.application.context_speed_progress.read_inputs", fail_if_called)
    shutil.rmtree(batch_dir)
    live_state = inspect_workspace(ws_a, results_root=results, reports_root=reports)
    assert live_state["bundles"][0]["phase"] == "run_missing"
