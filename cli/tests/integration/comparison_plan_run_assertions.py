"""Sealed-copy assertions for comparison stop priority and duplicate run ids."""

import asyncio
import json
import shutil
from collections.abc import Callable
from dataclasses import dataclass, replace
from pathlib import Path

from inferyard.contracts.comparison_types import JSONRecord
from inferyard.analysis.engine_comparison_v2 import evaluate_comparison
from inferyard.runtime.comparison_v2_read import read_comparison_v2
from inferyard.application.types import CommandRequest
from inferyard.application.verification import execute as verify
from inferyard.config.comparison_plan import comparison_slots
from inferyard.config.loader import LoadedConfig
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.errors import EvidenceError
from inferyard.contracts.validation import json_bytes
from inferyard.application.execution import execute_async
from inferyard.runtime.history_state import history, history_diagnostics
from inferyard.runtime.trial_runner import TrialDependencies
from tests.integration.comparison_plan_assertions import (
    _assert_not_selected,
    _reader_case,
    mutate_run_documents,
    run_paths,
)


@dataclass(frozen=True, slots=True)
class StopPriorityCase:
    name: str
    corrupt_fourth: bool


def assert_duplicate(tmp_path: Path, plan: JSONRecord, first: Path) -> None:
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
    _assert_not_selected(document, first.name)


def assert_stop_priority(
    tmp_path: Path,
    plan: JSONRecord,
    request: CommandRequest,
    deps: TrialDependencies,
    case: StopPriorityCase,
) -> None:
    batch = tmp_path / "batch"
    copy = tmp_path / case.name
    shutil.copytree(batch, copy)
    paths = run_paths(copy)
    second = paths[plan["trials"][1]["trial_id"]]
    fourth = paths[plan["trials"][3]["trial_id"]]
    shutil.rmtree(second)
    if case.corrupt_fourth:
        (fourth / "inputs.json").write_text("{}")
    else:
        def unplanned(run: JSONRecord, selection: JSONRecord) -> None:
            run["trial_id"] = "trial-unplanned-priority"
            selection["trial_id"] = "trial-unplanned-priority"

        mutate_run_documents(fourth, unplanned)
    document = read_comparison_v2(tmp_path / "plan", copy)
    stop = document["history_stop"]
    assert stop["trial_id"] == plan["trials"][1]["trial_id"]
    assert stop["detail"] == "subsequent_run_present"
    assert len(stop["subsequent_run_ids"]) >= 8
    for command in (
        replace(request, output_root=copy),
        CommandRequest("resume", from_run=fourth),
    ):
        code, result = asyncio.run(execute_async(command, deps))
        assert code == 3 and result.status == "comparison_history_stopped"
        comparison_stop = result.details["comparison_stop"]
        assert comparison_stop["trial_id"] == plan["trials"][1]["trial_id"]
        assert comparison_stop["detail"] == "subsequent_run_present"



def _duplicate_copy(
    tmp_path: Path,
    plan: JSONRecord,
    name: str,
    mutate: Callable[[JSONRecord, JSONRecord], None],
) -> tuple[Path, Path, Path | None, JSONRecord]:
    first_trial = plan["trials"][0]["trial_id"]
    slots = comparison_slots(plan)
    target_trial = next(
        slot.trial_id for slot in slots if slot.bundle_key != slots[0].bundle_key
    )
    copy = tmp_path / name
    shutil.copytree(tmp_path / "batch", copy)
    paths = run_paths(copy)
    source = paths[first_trial]
    target = paths.get(target_trial)
    duplicate = copy / "runs" / f"{name}-copy"
    shutil.copytree(source, duplicate)
    mutate_run_documents(duplicate, mutate)
    return copy, source, target, read_comparison_v2(tmp_path / "plan", copy)


def _duplicate_unplanned(tmp_path: Path, plan: JSONRecord) -> None:
    def unplanned(run: JSONRecord, selection: JSONRecord) -> None:
        run["trial_id"] = "trial-unplanned-xyz"
        selection["trial_id"] = "trial-unplanned-xyz"

    _, source, _, document = _duplicate_copy(tmp_path, plan, "dup-unplanned", unplanned)
    rows = [
        row
        for row in document["run_set"]["duplicates"]
        if row["detail"] == "duplicate_run_id"
    ]
    assert {row["trial_id"] for row in rows} == {
        plan["trials"][0]["trial_id"],
        "trial-unplanned-xyz",
    }
    assert document["run_set"]["unplanned"]
    _assert_not_selected(document, source.name)


def _duplicate_bundle(tmp_path: Path, plan: JSONRecord) -> None:
    slots = comparison_slots(plan)
    target_trial = next(
        slot.trial_id for slot in slots if slot.bundle_key != slots[0].bundle_key
    )

    def retrial(run: JSONRecord, selection: JSONRecord) -> None:
        run["trial_id"] = target_trial
        selection["trial_id"] = target_trial

    _, source, target, document = _duplicate_copy(tmp_path, plan, "dup-bundle", retrial)
    rows = [
        row
        for row in document["run_set"]["duplicates"]
        if row["detail"] == "duplicate_run_id"
    ]
    assert {row["trial_id"] for row in rows} == {
        plan["trials"][0]["trial_id"],
        target_trial,
    }
    _assert_not_selected(document, source.name)
    if target is not None:
        _assert_not_selected(document, target.name)


def _duplicate_same_trial(tmp_path: Path, plan: JSONRecord) -> None:
    first_trial = plan["trials"][0]["trial_id"]
    same = tmp_path / "dup-same-rejection"
    shutil.copytree(tmp_path / "batch", same)
    source = run_paths(same)[first_trial]
    duplicate = same / "runs" / "same-rejection-copy"
    shutil.copytree(source, duplicate)

    def wrong_plan(run: JSONRecord, _: JSONRecord) -> None:
        run["plan_sha256"] = "f" * 64

    mutate_run_documents(duplicate, wrong_plan)
    document = read_comparison_v2(tmp_path / "plan", same)
    assert any(
        row["detail"] == "duplicate_run_id" and row["trial_id"] == first_trial
        for row in document["run_set"]["duplicates"]
    )
    assert document["run_set"]["mismatched"]
    _assert_not_selected(document, source.name)


def _duplicate_wrong_plan_order(tmp_path: Path, plan: JSONRecord) -> None:
    def wrong_plan(run: JSONRecord, _: JSONRecord) -> None:
        run["plan_sha256"] = "f" * 64

    for name in ("000-wrong-copy", "zz-wrong-copy"):
        _, source, _, document = _duplicate_copy(tmp_path, plan, name, wrong_plan)
        assert any(
            row["detail"] == "run_plan_mismatch"
            for row in document["run_set"]["mismatched"]
        )
        assert len(
            [
                row
                for row in document["run_set"]["duplicates"]
                if row["detail"] == "duplicate_run_id"
            ]
        ) >= 2
        assert document["history_stop"]["detail"] == "run_plan_mismatch"
        _assert_not_selected(document, source.name)


def assert_duplicate_with_rejections(tmp_path: Path, plan: JSONRecord) -> None:
    _duplicate_unplanned(tmp_path, plan)
    _duplicate_bundle(tmp_path, plan)
    _duplicate_same_trial(tmp_path, plan)
    _duplicate_wrong_plan_order(tmp_path, plan)


def assert_batch_envelope_rejections(tmp_path: Path) -> None:
    batch = tmp_path / "batch"
    original = json.loads((batch / "batch.json").read_text())

    def schema_float(meta: JSONRecord) -> None:
        meta["schema_version"] = 3.0

    def diagnostic_int(meta: JSONRecord) -> None:
        meta["diagnostic"] = 0

    def empty_identity(meta: JSONRecord) -> None:
        meta["implementation_identity"] = {}

    def bad_tool(meta: JSONRecord) -> None:
        meta["tool_source_sha256"] = "0" * 63

    expected = {
        "schema-float": "invalid_artifact_version",
        "diagnostic-int": "batch_plan_identity_mismatch",
        "empty-identity": "batch_plan_identity_mismatch",
        "bad-tool": "batch_plan_identity_mismatch",
    }
    for name, mutate in (
        ("schema-float", schema_float),
        ("diagnostic-int", diagnostic_int),
        ("empty-identity", empty_identity),
        ("bad-tool", bad_tool),
    ):
        copy = tmp_path / f"envelope-{name}"
        shutil.copytree(batch, copy)
        meta = dict(original)
        mutate(meta)
        (copy / "batch.json").write_bytes(json_bytes(meta))
        document = read_comparison_v2(tmp_path / "plan", copy)
        assert document["eligible"] is False
        assert any(
            row["reason"] == "comparison_history_identity_error"
            and row["detail"] == "batch_envelope_invalid"
            and row.get("error")
            and "type" not in row["error"]
            for row in document["diagnostics"]
        )
        try:
            verify(CommandRequest("verify", run=copy))
        except EvidenceError as exc:
            assert str(exc) == expected[name]
        else:
            raise AssertionError(f"verify accepted invalid envelope {name}")


def _assert_duplicate_rows(
    rows: list[JSONRecord],
    source_trial: str,
    target_trial: str,
    source_name: str,
    copy_name: str,
) -> None:
    matched = [row for row in rows if row["detail"] == "duplicate_run_id"]
    assert {row["trial_id"] for row in matched} == {source_trial, target_trial}
    assert {row["run_id"] for row in matched} == {source_name}
    assert {Path(row["path"]).name for row in matched} == {source_name, copy_name}


def assert_cross_trial_duplicate(
    tmp_path: Path,
    plan: JSONRecord,
    source_trial: str,
    target_trial: str,
    copy_name: str,
) -> None:
    copy = tmp_path / f"cross-{copy_name}"
    shutil.copytree(tmp_path / "batch", copy)
    paths = run_paths(copy)
    source = paths[source_trial]
    target = paths[target_trial]
    duplicate = copy / "runs" / copy_name
    shutil.copytree(source, duplicate)

    def retrial(run: JSONRecord, selection: JSONRecord) -> None:
        run["trial_id"] = target_trial
        selection["trial_id"] = target_trial

    mutate_run_documents(duplicate, retrial)
    _, loaded = read_frozen_plan(tmp_path / "plan" / "plan.json")
    _, diagnostics = history_diagnostics(copy, plan, loaded, None)
    _assert_duplicate_rows(diagnostics, source_trial, target_trial, source.name, copy_name)
    document = read_comparison_v2(tmp_path / "plan", copy)
    _assert_duplicate_rows(
        document["run_set"]["duplicates"], source_trial, target_trial, source.name, copy_name
    )
    assert document["eligible"] is False
    _assert_not_selected(document, source.name)
    _assert_not_selected(document, target.name)


def assert_wrong_plan_stop_detail(
    tmp_path: Path,
    plan: JSONRecord,
    request: CommandRequest,
    deps: TrialDependencies,
) -> None:
    last_trial = plan["trials"][-1]["trial_id"]

    def wrong_plan(run: JSONRecord, _: JSONRecord) -> None:
        run["plan_sha256"] = "f" * 64

    _, _, target = _reader_case(tmp_path, plan, "wrong-plan-stop", last_trial, wrong_plan)
    for command in (
        replace(request, output_root=target.parent.parent),
        CommandRequest("resume", from_run=target),
    ):
        code, result = asyncio.run(execute_async(command, deps))
        assert code == 3 and result.status == "comparison_history_stopped"
        detail = result.details["comparison_stop"]["detail"]
        assert detail == "run_plan_mismatch", detail


def assert_layout1_case(
    batch: Path,
    plan: JSONRecord,
    loaded: dict[str, LoadedConfig],
    name: str,
    payload: JSONRecord,
) -> None:
    run_dir = batch / "runs" / "layout1"
    (run_dir / "run.json").write_bytes(json_bytes(payload))
    _, diagnostics = history_diagnostics(batch, plan, loaded, None)
    assert all(
        row.get("trial_id") is None
        and row["reason"] == "comparison_history_identity_error"
        and row.get("error")
        for row in diagnostics
    )
    document = read_comparison_v2(batch.parent / "plan", batch)
    assert all(row["reason"] != "EvidenceError" for row in document["diagnostics"])
    assert all(
        row.get("trial_id") != plan["trials"][0]["trial_id"]
        or row["reason"] == "planned_run_missing"
        for row in document["diagnostics"]
    )
    try:
        history(
            batch,
            plan,
            loaded,
            allow_tool_change=True,
            full_verification=True,
        )
    except EvidenceError as exc:
        assert str(exc) == {
            "damaged": "invalid_trial_input",
            "forged": "invalid_json_evidence",
        }[name]
    else:
        raise AssertionError("ordinary history must fail fast")


def assert_reader_preserves_read_detail(tmp_path: Path, plan: JSONRecord) -> None:
    first_trial = plan["trials"][0]["trial_id"]
    batch = tmp_path / "batch"
    copy = tmp_path / "reader-detail"
    shutil.copytree(batch, copy)
    source = run_paths(copy)[first_trial]
    events = source / "events.jsonl"
    events.write_bytes(events.read_bytes() + b"\n")
    document = read_comparison_v2(tmp_path / "plan", copy)
    detail = document["history_stop"]["detail"]
    assert detail == "original_evidence_hash_mismatch"
    for expected in (detail, "invalid_trial_input"):
        direct = evaluate_comparison(
            plan,
            [],
            evidence_errors=[
                {
                    "reason": "comparison_history_identity_error",
                    "trial_id": first_trial,
                    "run_id": source.name,
                    "path": str(source),
                    "detail": expected,
                    "error": {"reason": expected},
                }
            ],
        )
        assert direct["history_stop"]["detail"] == expected
