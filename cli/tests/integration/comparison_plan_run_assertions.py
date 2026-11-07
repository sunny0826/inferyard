"""Sealed-copy assertions for comparison stop priority and duplicate run ids."""

import asyncio
import shutil
from dataclasses import dataclass, replace
from pathlib import Path

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.analysis.engine_comparison_v2 import read_comparison_v2
from inferyard.application.types import CommandRequest
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.runtime.batch_runner import execute_async
from inferyard.runtime.history_state import history_diagnostics
from inferyard.runtime.trial_runner import TrialDependencies
from tests.integration.comparison_plan_assertions import (
    _assert_not_selected,
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
