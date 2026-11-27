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


def test_progress_handles_active_unsealed_local_engine_run(
    tmp_path: Path, config_path: Path, capsys
) -> None:
    from inferyard.analysis.scoring import ScoringContext
    from inferyard.cli import main
    from inferyard.config.loader import load_config
    from inferyard.config.single_plan import compile_single_plan
    from inferyard.contracts.validation import Document, strict_json_loads
    from inferyard.evidence.journal import TrialJournal
    from inferyard.evidence.request_snapshots import messages_sha256

    workspace = tmp_path / "ws"
    _init_workspace(workspace)
    _write_preflight(workspace)

    bundle_path = (workspace / "bundles" / "zh-core.json").resolve()
    bundle_dict = strict_json_loads(bundle_path.read_text(encoding="utf-8"))

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
            "family": "mlx-lm",
            "model": "/models/qwen",
            "bundle": str(bundle_path),
        },
    )
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

    config = load_config(config_path).config.to_dict()
    config["definition"] = "local-engine-config.v1"
    config["model"] = {
        "display_name": "Local model",
        "repo": "local",
        "revision": "frozen",
        "kind": "directory",
        "local_path": "model",
        "packing": "4bit",
        "template_path": "template.jinja",
        "template_sha256": "3" * 64,
    }
    config["engine"] = {
        "adapter": "local_openai_v1",
        "family": "mlx-lm",
        "backend": "metal",
        "installation_path": "runtime.json",
        "installation_sha256": "4" * 64,
        "startup_args": ["-I", "-B", "/installed/mlx_server.py", "--model", "/model"],
        "working_directory": "/runtime",
        "environment": {"PYTHONDONTWRITEBYTECODE": "1"},
        "served_model": "formal-local",
    }
    config["bundle"] = {"path": str(bundle_path), "version": bundle_dict["version"]}
    config["generation"] = {"temperature": 0, "max_tokens": 512}
    for field in ("threads", "threads_batch", "slots"):
        config["conditions"].pop(field, None)
    config["conditions"].update(cache_policy="unknown", ac_online=True, model_loaded=True)
    config["safety"] = {"interval_seconds": 0.5, "check_environment": True}
    config["output"]["root"] = str(workspace / "results")

    cfg_doc = Document.parse("config", config)
    bundle_doc = Document.parse("bundle", bundle_dict)
    plan = compile_single_plan(cfg_doc, bundle_doc)
    store = TrialJournal(
        workspace / "results",
        plan,
        plan["trials"][0]["trial_id"],
        config,
        bundle_dict,
        execution_mode="single",
        scoring_context=ScoringContext(),
    )
    try:
        store.observe(
            "engine_capabilities",
            {
                "definition": "local-engine-observation.v1",
                "engine": "local_openai_v1",
                "family": "mlx-lm",
                "mode": "native",
                "request_id_scope": "client_only",
                "engine_internal_drain": {
                    "value": None,
                    "scope": "capability_only",
                    "missing_reason": "native_lifecycle_unavailable",
                },
                "native_cancel": {
                    "value": None,
                    "scope": "capability_only",
                    "missing_reason": "native_lifecycle_unavailable",
                },
                "endpoints": {
                    "/v1/models": {
                        "available": True,
                        "status_code": 200,
                        "body": "{}",
                        "missing_reason": None,
                    }
                },
            },
            stage="pre_idle",
        )
        first_case = bundle_dict["cases"][0]
        req_id = f"{store.run_id}-formal-1"
        messages = [{"role": "user", "content": first_case["prompt"]}]
        body = {
            "model": "formal-local",
            "messages": messages,
            "stream": True,
            "temperature": 0,
            "max_tokens": 512,
        }
        store.prepare_request(req_id, "formal", 1, body)
        store.event(
            "request_started",
            "formal",
            req_id,
            {
                "case_id": first_case["case_id"],
                "plan_index": 0,
                "attempt": 1,
                "body": {
                    "model": "formal-local",
                    "messages_sha256": messages_sha256(messages),
                    "stream": True,
                    "generation": config["generation"],
                },
            },
        )
        # Simulate half-flushed JSONL tails written while streaming/sampling is in progress.
        with (store.path / "events.jsonl").open("ab") as partial_stream:
            partial_stream.write(b'{"schema_version": 3, "partial": ')
        with (store.path / "memory.jsonl").open("ab") as partial_mem:
            partial_mem.write(b'{"schema_version": 3, "partial_sample": ')

        state = inspect_workspace(workspace)
        core = next(b for b in state["bundles"] if b["bundle"] == "zh-core")
        assert core["phase"] == "run_incomplete"
        assert len(core["runs"]) == 1
        assert core["runs"][0]["complete"] is False
        assert core["runs"][0]["sealed"] is False

        code = main(["progress", "--workspace", str(workspace)])
        assert code == 0
        payload = json.loads(capsys.readouterr().out)
        assert payload["kind"] == "progress"

        # A newline-terminated corrupt record in memory.jsonl must still fail progress.
        (store.path / "memory.jsonl").write_bytes(b"{corrupt_complete_line}\n")
        assert main(["progress", "--workspace", str(workspace)]) == 2
        blocked = json.loads(capsys.readouterr().out)
        assert blocked["limitations"] == ["workspace_artifact_unreadable"]
    finally:
        for stream in store._logs.values():
            stream.close()


def test_progress_rejects_incompatible_unsealed_run_inputs(tmp_path: Path) -> None:
    workspace = tmp_path / "ws"
    _init_workspace(workspace)
    _write_preflight(workspace)
    _write_json(
        workspace / "results" / "20261127T000000Z-bad" / "inputs.json",
        {"schema_version": 2, "evidence_layout_version": 2},
    )

    code_err = None
    try:
        inspect_workspace(workspace)
    except Exception as exc:  # noqa: BLE001
        code_err = exc
    assert code_err is not None
