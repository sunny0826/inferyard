"""Reader rejection and evidence mutation helpers for comparison plan tests."""

import asyncio
import hashlib
import json
import shutil
from collections.abc import Callable
from dataclasses import replace
from pathlib import Path

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.analysis.engine_comparison_v2 import read_comparison_v2
from inferyard.application.types import CommandRequest
from inferyard.config.loader import LoadedConfig
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.runtime.batch_runner import execute_async
from inferyard.runtime.history_state import history, history_diagnostics
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


def assert_rejected_run_identity(
    tmp_path: Path,
    plan: JSONRecord,
    request: CommandRequest,
    deps: TrialDependencies,
) -> None:
    first = _first_slot_run(tmp_path / "batch", plan)
    first_trial = plan["trials"][0]["trial_id"]
    target_trial = plan["trials"][4]["trial_id"]
    _assert_identity_first(tmp_path, plan, first, request, deps)
    from tests.integration.comparison_plan_run_assertions import (
        assert_cross_trial_duplicate,
        assert_duplicate,
        assert_duplicate_with_rejections,
    )

    assert_duplicate(tmp_path, plan, first)
    assert_duplicate_with_rejections(tmp_path, plan)
    assert_cross_trial_duplicate(
        tmp_path, plan, first_trial, target_trial, "000-cross-copy"
    )
    assert_cross_trial_duplicate(
        tmp_path, plan, first_trial, target_trial, "zz-cross-copy"
    )


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


def run_paths(batch: Path) -> dict[str, Path]:
    return {
        read_trial(path)["run"]["trial_id"]: path
        for path in (batch / "runs").iterdir()
    }


def _retrial_jsonl(run_dir: Path, old: str, new: str) -> None:
    for path in run_dir.glob("*.jsonl"):
        rows: list[JSONRecord] = [json.loads(line) for line in path.read_text().splitlines()]
        changed = False
        for row in rows:
            if row.get("trial_id") == old:
                row["trial_id"] = new
                changed = True
        if changed:
            path.write_bytes(b"".join(json_bytes(row) for row in rows))
            _rehash_manifest(run_dir, path.name)


def mutate_run_documents(
    run_dir: Path, mutate: Callable[[JSONRecord, JSONRecord], None]
) -> None:
    inputs_path = run_dir / "inputs.json"
    inputs: JSONRecord = json.loads(inputs_path.read_text())
    run = inputs["documents"]["run"]["payload"]
    selection = inputs["documents"]["selection"]["payload"]
    old_trial = run["trial_id"]
    mutate(run, selection)
    if run["trial_id"] != old_trial:
        _retrial_jsonl(run_dir, old_trial, run["trial_id"])
    manifest_path = run_dir / "manifest.json"
    manifest: JSONRecord = json.loads(manifest_path.read_text())
    for key in ("run_id", "experiment_id", "trial_id", "origin", "kind", "execution_mode"):
        manifest[key] = run[key]
    manifest_path.write_bytes(json_bytes(manifest))
    for kind in ("run", "selection"):
        payload = inputs["documents"][kind]["payload"]
        inputs["documents"][kind]["sha256"] = hashlib.sha256(json_bytes(payload)).hexdigest()
    inputs_path.write_bytes(json_bytes(inputs))
    _rehash_manifest(run_dir, "inputs.json")


def _reader_case(
    tmp_path: Path,
    plan: JSONRecord,
    name: str,
    trial_id: str,
    mutate: Callable[[JSONRecord, JSONRecord], None],
) -> tuple[JSONRecord, list[JSONRecord], Path]:
    _, loaded = read_frozen_plan(tmp_path / "plan" / "plan.json")
    batch = tmp_path / "batch"
    copy = tmp_path / name
    shutil.copytree(batch, copy)
    target = run_paths(copy)[trial_id]
    mutate_run_documents(target, mutate)
    _, diagnostics = history_diagnostics(copy, plan, loaded, None)
    document = read_comparison_v2(tmp_path / "plan", copy)
    return document, diagnostics, target


def _assert_not_selected(document: JSONRecord, run_id: str) -> None:
    assert all(row.get("run_id") != run_id for row in document["handoff"]["slots"])
    for group in document["bundles"].values():
        assert run_id not in group["left"] and run_id not in group["right"]


def _assert_duplicate_initial(
    tmp_path: Path, plan: JSONRecord, first_trial: str, other_trial: str
) -> None:
    first_run_id = run_paths(tmp_path / "batch")[first_trial].name

    def duplicate_initial(run: JSONRecord, selection: JSONRecord) -> None:
        run["trial_id"] = first_trial
        selection["trial_id"] = first_trial

    document, diagnostics, target = _reader_case(
        tmp_path, plan, "relation-duplicate", other_trial, duplicate_initial
    )
    assert any(
        row["reason"] == "comparison_duplicate_trial_attempt"
        and row["trial_id"] == first_trial
        and row["run_id"] in (first_run_id, target.name)
        for row in diagnostics
    )
    assert "comparison_duplicate_trial_attempt" in {
        row["reason"] for row in document["diagnostics"]
    }
    assert document["run_set"]["duplicates"]
    assert document["eligible"] is False
    _assert_not_selected(document, target.name)


def _assert_unplanned(
    tmp_path: Path,
    plan: JSONRecord,
    second_trial: str,
    request: CommandRequest,
    deps: TrialDependencies,
) -> None:
    def unplanned(run: JSONRecord, selection: JSONRecord) -> None:
        run["trial_id"] = "trial-unplanned"
        selection["trial_id"] = "trial-unplanned"

    document, diagnostics, target = _reader_case(
        tmp_path, plan, "unplanned", second_trial, unplanned
    )
    assert any(
        row["reason"] == "comparison_unplanned_trial"
        and row["trial_id"] == "trial-unplanned"
        and row["run_id"] == target.name
        for row in diagnostics
    )
    unplanned_row = document["run_set"]["unplanned"][0]
    assert unplanned_row["detail"] == "unplanned_run_present"
    assert unplanned_row["error"]["reason"] == "trial_not_in_plan"
    assert any(
        row["reason"] == "comparison_unplanned_trial"
        and row["trial_id"] == "trial-unplanned"
        and row["error"]["reason"] == "trial_not_in_plan"
        for row in document["diagnostics"]
    )
    _assert_not_selected(document, target.name)
    limited = tmp_path / "unplanned-only"
    first_run_id = run_paths(tmp_path / "batch")[plan["trials"][0]["trial_id"]].name
    _keep_two_slots(target.parent.parent, limited, {first_run_id, target.name})
    limited_document = read_comparison_v2(tmp_path / "plan", limited)
    assert limited_document["history_stop"]["detail"] == "rejection_outside_planned_slots"
    for command in (
        replace(request, output_root=limited),
        CommandRequest("resume", from_run=limited / "runs" / target.name),
    ):
        code, result = asyncio.run(execute_async(command, deps))
        assert code == 3 and result.status == "comparison_history_stopped"
        assert result.details["comparison_stop"]["detail"] == "rejection_outside_planned_slots"



def _assert_wrong_plan(tmp_path: Path, plan: JSONRecord, second_trial: str) -> None:
    def wrong_plan(run: JSONRecord, _: JSONRecord) -> None:
        run["plan_sha256"] = "f" * 64

    document, diagnostics, target = _reader_case(
        tmp_path, plan, "wrong-plan", second_trial, wrong_plan
    )
    assert any(
        row["reason"] == "run_plan_mismatch"
        and row["trial_id"] == second_trial
        and row["run_id"] == target.name
        for row in diagnostics
    )
    assert any(
        row["reason"] == "run_plan_mismatch" and row["trial_id"] == second_trial
        for row in document["diagnostics"]
    )
    assert document["history_stop"]["detail"] == "run_plan_mismatch"
    _assert_not_selected(document, target.name)


def _assert_wrong_metadata(tmp_path: Path, plan: JSONRecord, second_trial: str) -> None:
    def wrong_experiment(run: JSONRecord, _: JSONRecord) -> None:
        run["experiment_id"] = "experiment-unplanned"

    def wrong_definition(run: JSONRecord, _: JSONRecord) -> None:
        run["definition_versions"] = {
            **run["definition_versions"],
            "scoring": "phase2.unexpected",
        }

    for name, mutate in (
        ("wrong-experiment", wrong_experiment),
        ("wrong-definition", wrong_definition),
    ):
        _, diagnostics, target = _reader_case(
            tmp_path, plan, name, second_trial, mutate
        )
        assert any(
            row["reason"] == "run_plan_mismatch"
            and row["trial_id"] == second_trial
            and row["run_id"] == target.name
            for row in diagnostics
        )


def _assert_invalid_resume(tmp_path: Path, plan: JSONRecord, second_trial: str) -> None:
    def invalid_resume(run: JSONRecord, selection: JSONRecord) -> None:
        run["relation"] = "resume"
        run["parent_run_id"] = "missing-parent"
        run["resumed_case_ids"] = list(selection["case_ids"])

    document, diagnostics, target = _reader_case(
        tmp_path, plan, "invalid-resume", second_trial, invalid_resume
    )
    assert any(
        row["reason"] == "comparison_history_identity_error"
        and row["trial_id"] == second_trial
        and row["run_id"] == target.name
        and row["error"] is not None
        for row in diagnostics
    )
    assert document["eligible"] is False


def _assert_unreadable(
    tmp_path: Path,
    plan: JSONRecord,
    second_trial: str,
    request: CommandRequest,
    deps: TrialDependencies,
) -> None:
    _, loaded = read_frozen_plan(tmp_path / "plan" / "plan.json")
    corrupt = tmp_path / "unreadable"
    shutil.copytree(tmp_path / "batch", corrupt)
    corrupt_run = run_paths(corrupt)[second_trial]
    (corrupt_run / "inputs.json").write_text("{}")
    _, diagnostics = history_diagnostics(corrupt, plan, loaded, None)
    assert any(row["reason"] == "comparison_history_identity_error" and row.get("error") for row in diagnostics)
    assert all(row.get("trial_id") != second_trial for row in diagnostics)
    document = read_comparison_v2(tmp_path / "plan", corrupt)
    assert all(row["reason"] != "EvidenceError" for row in document["diagnostics"])
    assert any(
        row["reason"] == "comparison_history_identity_error" and row.get("error")
        for row in document["diagnostics"]
    )
    first_run_id = run_paths(tmp_path / "batch")[plan["trials"][0]["trial_id"]].name
    limited = tmp_path / "unreadable-only"
    _keep_two_slots(corrupt, limited, {first_run_id, corrupt_run.name})
    limited_document = read_comparison_v2(tmp_path / "plan", limited)
    assert limited_document["history_stop"]["detail"] == "rejection_outside_planned_slots"
    for command in (
        replace(request, output_root=limited),
        CommandRequest("resume", from_run=limited / "runs" / corrupt_run.name),
    ):
        code, result = asyncio.run(execute_async(command, deps))
        assert code == 3 and result.status == "comparison_history_stopped"
    try:
        history(
            tmp_path / "wrong-definition",
            plan,
            loaded,
            allow_tool_change=True,
            full_verification=True,
        )
    except EvidenceError as exc:
        assert str(exc) == "trial_plan_binding_mismatch"
    else:
        raise AssertionError("ordinary history must fail fast")


def _keep_two_slots(source: Path, destination: Path, keep_names: set[str]) -> None:
    shutil.copytree(source, destination)
    for run in (destination / "runs").iterdir():
        if run.name not in keep_names:
            shutil.rmtree(run)


def _assert_layout1_fallback(tmp_path: Path, plan: JSONRecord) -> None:
    _, loaded = read_frozen_plan(tmp_path / "plan" / "plan.json")
    batch = tmp_path / "layout1-batch"
    (batch / "runs").mkdir(parents=True)
    shutil.copy(tmp_path / "batch" / "batch.json", batch / "batch.json")
    run_dir = batch / "runs" / "layout1"
    run_dir.mkdir()
    source = run_paths(tmp_path / "batch")[plan["trials"][0]["trial_id"]]
    original = json.loads((source / "inputs.json").read_text())["documents"]["run"]["payload"]
    (run_dir / "selection.json").write_bytes(json_bytes({"schema_version": 3}))
    for name, payload in (
        ("damaged", {"schema_version": 3}),
        (
            "forged",
            {
                **original,
                "run_id": "unvalidated-run",
                "trial_id": plan["trials"][0]["trial_id"],
                "plan_sha256": plan["plan_sha256"],
                "experiment_id": plan["experiment"]["experiment_id"],
                "definition_versions": plan["experiment"]["definition_versions"],
            },
        ),
    ):
        from tests.integration.comparison_plan_run_assertions import assert_layout1_case

        assert_layout1_case(batch, plan, loaded, name, payload)


def assert_real_reader_classification(
    tmp_path: Path,
    plan: JSONRecord,
    request: CommandRequest,
    deps: TrialDependencies,
) -> None:
    first_trial = plan["trials"][0]["trial_id"]
    second_trial = plan["trials"][1]["trial_id"]
    workloads = {row["workload_id"]: row for row in plan["experiment"]["workloads"]}
    first_config = workloads[plan["trials"][0]["workload_id"]]["config"]["sha256"]
    other_trial = next(
        trial["trial_id"]
        for trial in plan["trials"][1:]
        if workloads[trial["workload_id"]]["config"]["sha256"] == first_config
    )
    _assert_duplicate_initial(tmp_path, plan, first_trial, other_trial)
    _assert_unplanned(tmp_path, plan, second_trial, request, deps)
    from tests.integration.comparison_plan_run_assertions import (
        StopPriorityCase,
        assert_batch_envelope_rejections,
        assert_stop_priority,
    )

    from tests.integration.comparison_plan_run_assertions import (
        assert_reader_preserves_read_detail,
    )

    assert_reader_preserves_read_detail(tmp_path, plan)
    assert_batch_envelope_rejections(tmp_path)
    assert_stop_priority(
        tmp_path, plan, request, deps, StopPriorityCase("priority-unplanned", False)
    )
    assert_stop_priority(
        tmp_path, plan, request, deps, StopPriorityCase("priority-unreadable", True)
    )
    _assert_wrong_plan(tmp_path, plan, second_trial)
    _assert_wrong_metadata(tmp_path, plan, second_trial)
    _assert_invalid_resume(tmp_path, plan, second_trial)
    _assert_unreadable(tmp_path, plan, second_trial, request, deps)
    _assert_layout1_fallback(tmp_path, plan)
