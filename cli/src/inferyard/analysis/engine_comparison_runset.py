"""Frozen v2 run-set classification, selection and stop assembly (ADR 057c)."""

from __future__ import annotations

from inferyard.analysis.engine_comparison_bundles import bundle_groups
from inferyard.analysis.engine_comparison_handoff import handoff_rows
from inferyard.analysis.engine_comparison_services import service_diagnostics
from inferyard.analysis.comparison_types import JSONRecord
from inferyard.analysis.engine_comparison_shared import (
    HANDOFF_BROKEN,
    HANDOFF_MISSING,
    HANDOFF_SLOT_MISMATCH,
    PLAN_HASH_MISMATCH,
    PLANNED_RUN_MISSING,
    SERVICE_IDENTITY_INVALID,
    SERVICE_IDENTITY_MISSING,
    SERVICE_NOT_REPLACED,
    append_diagnostic,
    slot_label,
)
from inferyard.analysis.comparison_history import (
    DUPLICATE_ATTEMPT,
    HISTORY_IDENTITY_ERROR,
    REJECTION_CLASSIFICATIONS,
    RUN_PLAN_MISMATCH,
    UNPLANNED_TRIAL,
    comparison_history_stop,
)
from inferyard.config.comparison_plan import Slot

__all__ = [
    "HANDOFF_BROKEN",
    "HANDOFF_MISSING",
    "HANDOFF_SLOT_MISMATCH",
    "PLAN_HASH_MISMATCH",
    "PLANNED_RUN_MISSING",
    "SERVICE_IDENTITY_INVALID",
    "SERVICE_IDENTITY_MISSING",
    "SERVICE_NOT_REPLACED",
    "append_diagnostic",
    "build_run_set",
]


def _plan_mismatch_reason(entry: JSONRecord, plan: JSONRecord) -> str | None:
    run, experiment = entry["run"], plan["experiment"]
    if (
        run["plan_sha256"] != plan["plan_sha256"]
        or run["experiment_id"] != experiment["experiment_id"]
        or run["definition_versions"] != experiment["definition_versions"]
    ):
        return RUN_PLAN_MISMATCH
    return None


def _observed_label(run: JSONRecord, entry: JSONRecord, slot: Slot | None) -> JSONRecord:
    return {
        "trial_id": run["trial_id"],
        "run_id": run["run_id"],
        "path": str(entry.get("path")),
        "workload_id": None if slot is None else slot.workload_id,
        "block": None if slot is None else slot.block,
        "side": None if slot is None else slot.side,
        "bundle_key": None if slot is None else slot.bundle_key,
    }


def _reject_duplicate_run_id(
    run: JSONRecord,
    entry: JSONRecord,
    run_set: JSONRecord,
    rejected: set[str],
    reported: set[tuple[str, str]],
) -> None:
    path = str(entry.get("path"))
    key = (run["run_id"], path)
    if key not in reported:
        reported.add(key)
        run_set["duplicates"].append(
            {
                "trial_id": run["trial_id"],
                "run_id": run["run_id"],
                "path": path,
                "detail": "duplicate_run_id",
            }
        )
    rejected.add(run["trial_id"])


def _register_identity(entry: JSONRecord, state: JSONRecord) -> None:
    run = entry["run"]
    if run["run_id"] is None:
        return
    seen, rejected = state["seen"], state["rejected"]
    reported = state["duplicate_paths"]
    if run["run_id"] in seen:
        original = seen[run["run_id"]]
        _reject_duplicate_run_id(original["run"], original, state["run_set"], rejected, reported)
        _reject_duplicate_run_id(run, entry, state["run_set"], rejected, reported)
        return
    seen[run["run_id"]] = entry


def _classify_entry(
    plan: JSONRecord,
    slots: tuple[Slot, ...],
    index: dict[str, int],
    entry: JSONRecord,
    state: JSONRecord,
) -> None:
    run_set, by_trial = state["run_set"], state["by_trial"]
    seen, rejected = state["seen"], state["rejected"]
    run = entry["run"]
    position = index.get(run["trial_id"])
    slot = None if position is None else slots[position]
    label = _observed_label(run, entry, slot)
    run_set["observed_runs"].append(label)
    if entry.get("identity_error"):
        run_set["mismatched"].append(
            {
                **label,
                "detail": entry["identity_error"],
                "error": entry.get("identity_error_detail"),
            }
        )
        rejected.add(run["trial_id"])
        _register_identity(entry, state)
        return
    if position is None:
        run_set["unplanned"].append(
            {"trial_id": run["trial_id"], "run_id": run["run_id"], "path": str(entry.get("path"))}
        )
        _register_identity(entry, state)
        return
    reason = _plan_mismatch_reason(entry, plan)
    duplicate = run["run_id"] in seen
    _register_identity(entry, state)
    if reason is not None:
        run_set["mismatched"].append(
            {
                "trial_id": run["trial_id"],
                "run_id": run["run_id"],
                "path": str(entry.get("path")),
                "detail": reason,
            }
        )
        rejected.add(run["trial_id"])
        return
    if duplicate:
        return
    by_trial.setdefault(run["trial_id"], []).append(entry)


def _classify_runs(
    plan: JSONRecord, slots: tuple[Slot, ...], runs: list[JSONRecord]
) -> tuple[JSONRecord, dict[str, list[JSONRecord]], set[str]]:
    run_set = {
        "expected_trials": [slot.trial_id for slot in slots],
        "observed_runs": [],
        "missing": [],
        "unplanned": [],
        "mismatched": [],
        "duplicates": [],
    }
    index = {slot.trial_id: position for position, slot in enumerate(slots)}
    by_trial, seen, rejected = {}, {}, set()
    state = {
        "run_set": run_set,
        "by_trial": by_trial,
        "seen": seen,
        "rejected": rejected,
        "duplicate_paths": set(),
    }
    for entry in runs:
        _classify_entry(plan, slots, index, entry, state)
    return run_set, by_trial, rejected, state


def _reject_unplanned(run_set: JSONRecord, rejection: JSONRecord) -> None:
    run_set["unplanned"].append(
        {
            "trial_id": rejection.get("trial_id"),
            "run_id": rejection.get("run_id"),
            "path": rejection.get("path"),
            "detail": "unplanned_run_present",
            "error": rejection.get("error"),
        }
    )


def _reject_duplicate_attempt(
    run_set: JSONRecord,
    slot: Slot,
    rejection: JSONRecord,
    reported: set[tuple[str, str]],
) -> None:
    path = str(rejection.get("path"))
    key = (rejection.get("run_id"), path)
    if key in reported:
        return
    reported.add(key)
    run_set["duplicates"].append(
        {
            "trial_id": slot.trial_id,
            "run_id": rejection.get("run_id"),
            "path": path,
            "detail": rejection.get("detail") or "duplicate_trial_attempt",
        }
    )


def _reject_mismatch(
    run_set: JSONRecord, slot: Slot, rejection: JSONRecord
) -> None:
    reason = rejection.get("reason")
    detail = rejection.get("detail")
    run_set["mismatched"].append(
        {
            "trial_id": slot.trial_id,
            "run_id": rejection.get("run_id"),
            "path": rejection.get("path"),
            "detail": RUN_PLAN_MISMATCH if reason == RUN_PLAN_MISMATCH else (
                detail or "run_unreadable"
            ),
            "error": rejection.get("error"),
        }
    )


def _classify_rejections(
    slots: tuple[Slot, ...],
    rejections: tuple[JSONRecord, ...] | list[JSONRecord],
    diagnostics: list[JSONRecord],
    state: JSONRecord,
) -> None:
    run_set, rejected = state["run_set"], state["rejected"]
    by_trial = {slot.trial_id: slot for slot in slots}
    for rejection in rejections:
        trial_id = rejection.get("trial_id")
        reason = rejection.get("reason")
        slot = by_trial.get(trial_id)
        _register_identity(
            {
                "run": {"trial_id": trial_id, "run_id": rejection.get("run_id")},
                "path": rejection.get("path"),
            },
            state,
        )
        if reason == UNPLANNED_TRIAL:
            _reject_unplanned(run_set, rejection)
            continue
        if slot is None:
            canonical = reason if reason in REJECTION_CLASSIFICATIONS else HISTORY_IDENTITY_ERROR
            append_diagnostic(
                diagnostics,
                canonical,
                trial_id=trial_id,
                run_id=rejection.get("run_id"),
                path=rejection.get("path"),
                detail=rejection.get("detail"),
                error=rejection.get("error"),
            )
            continue
        rejected.add(slot.trial_id)
        run_set["observed_runs"].append(
            {
                **slot_label(slot),
                "run_id": rejection.get("run_id"),
                "path": rejection.get("path"),
            }
        )
        if reason == DUPLICATE_ATTEMPT:
            _reject_duplicate_attempt(
                run_set, slot, rejection, state["duplicate_paths"]
            )
        else:
            _reject_mismatch(run_set, slot, rejection)


def _select_slots(
    run_set: JSONRecord,
    slots: tuple[Slot, ...],
    by_trial: dict[str, list[JSONRecord]],
    rejected: set[str],
) -> dict[str, JSONRecord]:
    selected = {}
    for slot in slots:
        if slot.trial_id in rejected:
            continue
        attempts = by_trial.get(slot.trial_id, [])
        if not attempts:
            if slot.trial_id not in rejected:
                run_set["missing"].append(slot_label(slot))
        elif len(attempts) != 1:
            run_set["duplicates"].append(
                {
                    "trial_id": slot.trial_id,
                    "run_ids": [entry["run"]["run_id"] for entry in attempts],
                    "detail": "multiple_attempts",
                }
            )
        else:
            selected[slot.trial_id] = attempts[0]
    return selected


def _trim_missing_tails(run_set: JSONRecord, stop_trial_id: str | None) -> None:
    if stop_trial_id is None:
        return
    positions = {trial_id: index for index, trial_id in enumerate(run_set["expected_trials"])}
    stop_index = positions.get(stop_trial_id)
    if stop_index is None:
        return
    run_set["missing"] = [
        row for row in run_set["missing"] if positions.get(row["trial_id"], -1) <= stop_index
    ]


def _run_set_diagnostics(run_set: JSONRecord, diagnostics: list[JSONRecord]) -> None:
    for row in run_set["missing"]:
        append_diagnostic(
            diagnostics,
            PLANNED_RUN_MISSING,
            trial_id=row["trial_id"],
            workload_id=row["workload_id"],
        )
    for row in run_set["unplanned"]:
        append_diagnostic(
            diagnostics,
            UNPLANNED_TRIAL,
            trial_id=row["trial_id"],
            run_id=row["run_id"],
            detail=row.get("detail") or "unplanned_run_present",
            error=row.get("error"),
        )
    for row in run_set["mismatched"]:
        append_diagnostic(
            diagnostics,
            RUN_PLAN_MISMATCH if row["detail"] == RUN_PLAN_MISMATCH else HISTORY_IDENTITY_ERROR,
            trial_id=row["trial_id"],
            run_id=row["run_id"],
            detail=row["detail"],
            error=row.get("error"),
        )
    for row in run_set["duplicates"]:
        append_diagnostic(
            diagnostics,
            DUPLICATE_ATTEMPT,
            trial_id=row["trial_id"],
            run_id=row.get("run_id"),
            detail=row["detail"],
        )


def _classification_rejections(run_set: JSONRecord) -> list[JSONRecord]:
    rows = [
        {
            "reason": RUN_PLAN_MISMATCH
            if row["detail"] == RUN_PLAN_MISMATCH
            else HISTORY_IDENTITY_ERROR,
            **row,
        }
        for row in run_set["mismatched"]
    ]
    rows += [{"reason": UNPLANNED_TRIAL, **row} for row in run_set["unplanned"]]
    rows += [
        {"reason": DUPLICATE_ATTEMPT, **row}
        for row in run_set["duplicates"]
        if row["detail"] == "duplicate_run_id"
    ]
    return rows


def build_run_set(
    plan: JSONRecord,
    slots: tuple[Slot, ...],
    runs: list[JSONRecord],
    rejections: tuple[JSONRecord, ...] | list[JSONRecord],
    *,
    plan_hash_ok: bool,
) -> JSONRecord:
    diagnostics: list[JSONRecord] = []
    if not plan_hash_ok:
        append_diagnostic(diagnostics, PLAN_HASH_MISMATCH, detail="batch_plan_mismatch")
    run_set, by_trial, rejected, state = _classify_runs(plan, slots, runs)
    _classify_rejections(slots, rejections, diagnostics, state)
    history_stop = comparison_history_stop(
        plan, runs, [*rejections, *_classification_rejections(run_set)]
    )
    selected = _select_slots(run_set, slots, by_trial, rejected)
    _trim_missing_tails(run_set, None if history_stop is None else history_stop.get("trial_id"))
    _run_set_diagnostics(run_set, diagnostics)
    service_diagnostics(slots, selected, diagnostics)
    handoff = handoff_rows(slots, selected, diagnostics)
    if history_stop is not None:
        append_diagnostic(
            diagnostics,
            history_stop["reason"],
            trial_id=history_stop.get("trial_id"),
            run_id=history_stop.get("run_id"),
            detail=history_stop.get("detail"),
            error=history_stop.get("error"),
        )
    return {
        "run_set": run_set,
        "handoff": {
            "slots": handoff,
            "blockers": [
                row
                for row in handoff
                if row["status"] not in ("ok", "not_checked", "not_selected")
            ],
        },
        "history_stop": history_stop,
        "bundles": bundle_groups(plan, slots, selected),
        "diagnostics": diagnostics,
    }
