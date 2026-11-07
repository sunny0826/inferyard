"""Direct-entry duplicate run-id stop and evidence alignment."""

from copy import deepcopy
from pathlib import Path

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.analysis.engine_comparison_v2 import evaluate_comparison
from tests.unit.test_engine_comparison_v2 import plan_with_slots, synthetic_runs


def _selected(document: JSONRecord) -> set[str]:
    return {
        row["run_id"]
        for row in document["handoff"]["slots"]
        if row["run_id"] is not None
    }


def _duplicate_rows(document: JSONRecord) -> list[JSONRecord]:
    return [
        row
        for row in document["run_set"]["duplicates"]
        if row["detail"] == "duplicate_run_id"
    ]


def test_renamed_duplicate_stops_at_earliest_rejected_slot(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[4]["run"]["run_id"] = runs[0]["run"]["run_id"]
    document = evaluate_comparison(plan, runs)
    assert document["history_stop"]["trial_id"] == plan["trials"][0]["trial_id"]
    assert document["history_stop"]["detail"] == "duplicate_run_id"
    assert runs[0]["run"]["run_id"] not in _selected(document)


def test_appended_duplicate_stops_at_original_slot(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    duplicate = deepcopy(runs[0])
    duplicate["run"]["trial_id"] = runs[4]["run"]["trial_id"]
    duplicate["path"] = "/runs/appended-duplicate"
    runs.append(duplicate)
    document = evaluate_comparison(plan, runs)
    assert document["history_stop"]["trial_id"] == plan["trials"][0]["trial_id"]
    assert document["history_stop"]["detail"] == "duplicate_run_id"


def test_three_same_ids_keep_one_evidence_row_per_path(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[4]["run"]["run_id"] = runs[0]["run"]["run_id"]
    extra = deepcopy(runs[0])
    extra["run"]["trial_id"] = runs[9]["run"]["trial_id"]
    extra["path"] = "/runs/extra-duplicate"
    runs.append(extra)
    document = evaluate_comparison(plan, runs)
    rows = _duplicate_rows(document)
    assert len(rows) == 3
    assert len({row["path"] for row in rows}) == 3
    assert {row["trial_id"] for row in rows} == {
        plan["trials"][0]["trial_id"],
        plan["trials"][4]["trial_id"],
        plan["trials"][9]["trial_id"],
    }
