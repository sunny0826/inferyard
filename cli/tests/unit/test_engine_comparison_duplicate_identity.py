"""Direct-entry duplicate run-id stop and evidence alignment."""

from copy import deepcopy
from pathlib import Path

from inferyard.contracts.comparison_types import JSONRecord
from inferyard.analysis.comparison_history import comparison_history_stop
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


def _rejection(reason: str, trial_id: str, run_id: str, detail: str) -> JSONRecord:
    return {
        "reason": reason,
        "trial_id": trial_id,
        "run_id": run_id,
        "path": "/runs/evidence-copy",
        "detail": detail,
        "error": {"reason": reason},
    }


def _assert_source_rejected(document: JSONRecord, run_id: str) -> None:
    assert run_id not in _selected(document)


def test_direct_wrong_plan_rejection_stops_at_slot(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[3]["run"]["plan_sha256"] = "f" * 64
    document = evaluate_comparison(plan, runs)
    assert document["history_stop"]["trial_id"] == plan["trials"][3]["trial_id"]
    assert document["history_stop"]["detail"] == "run_plan_mismatch"
    _assert_source_rejected(document, runs[3]["run"]["run_id"])


def test_direct_identity_error_rejection_stops_at_slot(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    runs[3]["identity_error"] = "identity_error"
    runs[3]["identity_error_detail"] = {"reason": "identity_error"}
    document = evaluate_comparison(plan, runs)
    assert document["history_stop"]["trial_id"] == plan["trials"][3]["trial_id"]
    assert document["history_stop"]["detail"] == "identity_error"
    _assert_source_rejected(document, runs[3]["run"]["run_id"])


def test_direct_unplanned_rejection_stops_batch(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    extra = deepcopy(runs[0])
    extra["run"]["run_id"] = "run-extra"
    extra["run"]["trial_id"] = "trial-extra"
    extra["path"] = "/runs/extra"
    document = evaluate_comparison(plan, [*runs, extra])
    assert document["history_stop"]["trial_id"] == "trial-extra"
    assert document["history_stop"]["detail"] == "rejection_outside_planned_slots"


def test_dual_input_unplanned_duplicate_rejects_source(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    source = runs[0]["run"]["run_id"]
    error = _rejection(
        "comparison_unplanned_trial",
        "trial-extra",
        source,
        "unplanned_run_present",
    )
    document = evaluate_comparison(plan, runs, evidence_errors=[error])
    rows = _duplicate_rows(document)
    assert {row["trial_id"] for row in rows} == {
        plan["trials"][0]["trial_id"],
        "trial-extra",
    }
    _assert_source_rejected(document, source)


def test_dual_input_wrong_plan_duplicate_rejects_source(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    source = runs[0]["run"]["run_id"]
    error = _rejection(
        "run_plan_mismatch", plan["trials"][4]["trial_id"], source, "run_plan_mismatch"
    )
    document = evaluate_comparison(plan, runs, evidence_errors=[error])
    assert {row["trial_id"] for row in _duplicate_rows(document)} == {
        plan["trials"][0]["trial_id"],
        plan["trials"][4]["trial_id"],
    }
    _assert_source_rejected(document, source)


def test_dual_input_identity_error_duplicate_rejects_source(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    source = runs[0]["run"]["run_id"]
    error = _rejection(
        "comparison_history_identity_error",
        plan["trials"][4]["trial_id"],
        source,
        "identity_error",
    )
    document = evaluate_comparison(plan, runs, evidence_errors=[error])
    assert {row["trial_id"] for row in _duplicate_rows(document)} == {
        plan["trials"][0]["trial_id"],
        plan["trials"][4]["trial_id"],
    }
    _assert_source_rejected(document, source)


def test_duplicate_wrong_plan_keeps_mismatch_in_both_orders(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    source = synthetic_runs(plan)
    copy = deepcopy(source[0])
    copy["run"]["plan_sha256"] = "f" * 64
    copy["path"] = "/runs/ordered-copy"
    for runs in (source + [copy], [copy, *source]):
        document = evaluate_comparison(plan, runs)
        assert any(
            row["detail"] == "run_plan_mismatch"
            for row in document["run_set"]["mismatched"]
        )
        assert document["history_stop"]["detail"] == "run_plan_mismatch"
        assert len({row["path"] for row in _duplicate_rows(document)}) == 2
        _assert_source_rejected(document, copy["run"]["run_id"])


def test_direct_unplanned_identity_error_stops_batch(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    extra = deepcopy(runs[0])
    extra["run"]["run_id"] = "run-unplanned-error"
    extra["run"]["trial_id"] = "trial-unplanned-error"
    extra["identity_error"] = "invalid_trial_input"
    extra["identity_error_detail"] = {"reason": "invalid_trial_input"}
    document = evaluate_comparison(plan, [*runs, extra])
    assert document["history_stop"]["trial_id"] == "trial-unplanned-error"
    assert document["history_stop"]["detail"] == "rejection_outside_planned_slots"


def test_direct_null_trial_identity_error_stops_batch(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    extra = deepcopy(runs[0])
    extra["run"]["run_id"] = "run-null-trial-error"
    extra["run"]["trial_id"] = None
    extra["identity_error"] = "invalid_trial_input"
    extra["identity_error_detail"] = {"reason": "invalid_trial_input"}
    document = evaluate_comparison(plan, [*runs, extra])
    assert document["history_stop"].get("trial_id") is None
    assert document["history_stop"]["detail"] == "rejection_outside_planned_slots"


def test_same_slot_priority_is_order_independent(tmp_path: Path) -> None:
    plan, _ = plan_with_slots(tmp_path)
    runs = synthetic_runs(plan)
    trial = plan["trials"][0]["trial_id"]
    run_id = runs[0]["run"]["run_id"]
    wrong = {
        "trial_id": trial,
        "run_id": run_id,
        "path": runs[0]["path"],
        "reason": "run_plan_mismatch",
        "detail": "run_plan_mismatch",
    }
    duplicate = {
        "trial_id": trial,
        "run_id": run_id,
        "path": runs[0]["path"],
        "reason": "comparison_duplicate_trial_attempt",
        "detail": "duplicate_run_id",
    }
    for rejections in ([wrong, duplicate], [duplicate, wrong]):
        stop = comparison_history_stop(plan, runs, rejections)
        assert stop["detail"] == "run_plan_mismatch"
