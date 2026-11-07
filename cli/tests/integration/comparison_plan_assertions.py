"""Reader rejection and evidence mutation helpers for comparison plan tests."""

import asyncio
import hashlib
import json
import shutil
from dataclasses import replace
from pathlib import Path

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.analysis.engine_comparison_v2 import read_comparison_v2
from inferyard.application.types import CommandRequest
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import json_bytes
from inferyard.runtime.batch_runner import execute_async
from inferyard.runtime.trial_runner import TrialDependencies


def _first_slot_run(batch: Path, plan: JSONRecord) -> Path:
    trial_id = plan["trials"][0]["trial_id"]
    for path in (batch / "runs").iterdir():
        if read_trial(path)["run"]["trial_id"] == trial_id:
            return path
    raise AssertionError("first frozen slot has no run")


def _assert_identity_first(
    tmp_path: Path,
    plan: JSONRecord,
    first: Path,
    request: CommandRequest,
    deps: TrialDependencies,
) -> None:
    batch = tmp_path / "batch"
    slot = plan["trials"][0]
    identity_first = tmp_path / "identity-first"
    (identity_first / "runs").mkdir(parents=True)
    shutil.copy(batch / "batch.json", identity_first / "batch.json")
    shutil.copytree(batch / "frozen-plan", identity_first / "frozen-plan")
    target = identity_first / "runs" / first.name
    shutil.copytree(first, target)
    _mutate_config_drift(target)
    rejected = read_comparison_v2(tmp_path / "plan", identity_first)
    assert rejected["history_stop"]["trial_id"] == slot["trial_id"]
    assert rejected["history_stop"]["run_id"] == first.name
    assert rejected["history_stop"]["reason"] == "comparison_history_identity_error"
    assert rejected["history_stop"]["detail"] == "batch_frozen_inputs_changed"
    assert rejected["history_stop"]["error"] == {"reason": "batch_frozen_inputs_changed"}
    assert rejected["run_set"]["observed_runs"][0]["trial_id"] == slot["trial_id"]
    assert rejected["run_set"]["missing"] == []
    for command in (
        replace(request, output_root=identity_first),
        CommandRequest("resume", from_run=target),
    ):
        code, stopped = asyncio.run(execute_async(command, deps))
        comparison_stop = stopped.details["comparison_stop"]
        assert code == 3 and stopped.status == "comparison_history_stopped"
        assert comparison_stop["trial_id"] == slot["trial_id"]
        assert comparison_stop["run_id"] == first.name
        assert comparison_stop["detail"] == "batch_frozen_inputs_changed"
        assert comparison_stop["error"] == {"reason": "batch_frozen_inputs_changed"}


def _assert_duplicate(
    tmp_path: Path, plan: JSONRecord, first: Path
) -> None:
    batch = tmp_path / "batch"
    slot = plan["trials"][0]
    duplicate = tmp_path / "duplicate-batch"
    shutil.copytree(batch, duplicate)
    shutil.copytree(first, duplicate / "runs" / "extra-copy")
    document = read_comparison_v2(tmp_path / "plan", duplicate)
    assert document["eligible"] is False
    assert document["run_set"]["duplicates"][0]["run_id"] == first.name
    assert document["run_set"]["duplicates"][0]["detail"] == "duplicate_run_id"
    assert document["history_stop"]["trial_id"] == slot["trial_id"]
    assert document["history_stop"]["run_id"] == first.name


def assert_rejected_run_identity(
    tmp_path: Path,
    plan: JSONRecord,
    request: CommandRequest,
    deps: TrialDependencies,
) -> None:
    first = _first_slot_run(tmp_path / "batch", plan)
    _assert_identity_first(tmp_path, plan, first, request, deps)
    _assert_duplicate(tmp_path, plan, first)


def _rehash_manifest(run_dir: Path, filename: str) -> None:
    manifest_path = run_dir / "manifest.json"
    manifest: JSONRecord = json.loads(manifest_path.read_text())
    raw = (run_dir / filename).read_bytes()
    entry = manifest["files"][filename]
    entry["sha256"] = hashlib.sha256(raw).hexdigest()
    entry["bytes"] = len(raw)
    manifest_path.write_bytes(json_bytes(manifest))


def _mutate_config_drift(run_dir: Path) -> None:
    inputs_path = run_dir / "inputs.json"
    inputs: JSONRecord = json.loads(inputs_path.read_text())
    config = inputs["documents"]["config"]["payload"]
    config["device"]["id"] = "LAB-DRIFTED"
    payload = json_bytes(config)
    inputs["documents"]["config"] = {
        "payload": config,
        "sha256": hashlib.sha256(payload).hexdigest(),
    }
    selection = inputs["documents"]["selection"]["payload"]
    selection["config_sha256"] = inputs["documents"]["config"]["sha256"]
    inputs["documents"]["selection"]["sha256"] = hashlib.sha256(
        json_bytes(selection)
    ).hexdigest()
    inputs_path.write_bytes(json_bytes(inputs))
    _rehash_manifest(run_dir, "inputs.json")


def _mutate_endpoint(run_dir: Path) -> None:
    path = run_dir / "observations.jsonl"
    rows: list[JSONRecord] = [json.loads(line) for line in path.read_text().splitlines()]
    for row in rows:
        if row["kind"] == "service_binding":
            row["payload"]["effective_endpoint"]["server_pid"] += 1
            row["payload_sha256"] = hashlib.sha256(json_bytes(row["payload"])).hexdigest()
            break
    path.write_bytes(b"".join(json_bytes(row) for row in rows))
    _rehash_manifest(run_dir, "observations.jsonl")


def assert_drift_and_endpoint_rejection(
    tmp_path: Path,
    request: CommandRequest,
    deps: TrialDependencies,
) -> None:
    batch = tmp_path / "batch"
    drift = tmp_path / "drift-batch"
    shutil.copytree(batch, drift)
    run_dir = next((drift / "runs").iterdir())
    _mutate_config_drift(run_dir)
    rejected = read_comparison_v2(tmp_path / "plan", drift)
    assert rejected["eligible"] is False
    assert any(
        row["detail"] == "batch_frozen_inputs_changed" for row in rejected["diagnostics"]
    )
    code, stopped = asyncio.run(
        execute_async(replace(request, output_root=drift), deps)
    )
    assert code == 3 and stopped.status == "comparison_history_stopped"
    assert stopped.details["comparison_stop"]["reason"] == "comparison_history_identity_error"
    resume = CommandRequest("resume", from_run=run_dir)
    code, stopped = asyncio.run(execute_async(resume, deps))
    assert code == 3 and stopped.status == "comparison_history_stopped"

    endpoint = tmp_path / "endpoint-batch"
    shutil.copytree(batch, endpoint)
    endpoint_run = next((endpoint / "runs").iterdir())
    _mutate_endpoint(endpoint_run)
    rejected = read_comparison_v2(tmp_path / "plan", endpoint)
    assert rejected["eligible"] is False
    assert any(
        row.get("detail") == "service_binding_differs_from_execution"
        for row in rejected["diagnostics"]
    )
