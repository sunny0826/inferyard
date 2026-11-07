"""Comparison-specific history stop shared by execution and v2 derivation (ADR 057c).

The stop reads only frozen slot order. It never treats a normal handoff as a run
terminal state, and model answer quality is outside execution stopping.
"""

from __future__ import annotations

from dataclasses import dataclass

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.config.comparison_plan import Slot, comparison_slots

HISTORY_IDENTITY_ERROR = "comparison_history_identity_error"
HISTORY_CANCELLED = "comparison_history_cancelled"
HISTORY_FAILED = "comparison_history_failed"
HISTORY_INCOMPLETE = "comparison_history_incomplete"
UNPLANNED_TRIAL = "comparison_unplanned_trial"
RUN_PLAN_MISMATCH = "run_plan_mismatch"
DUPLICATE_ATTEMPT = "comparison_duplicate_trial_attempt"
REJECTION_CLASSIFICATIONS = frozenset(
    {HISTORY_IDENTITY_ERROR, UNPLANNED_TRIAL, RUN_PLAN_MISMATCH, DUPLICATE_ATTEMPT}
)


@dataclass(frozen=True, slots=True)
class StopEvidence:
    subsequent: tuple[str, ...] = ()
    run_id: str | None = None
    error: JSONRecord | None = None


def _entry_label(slot: Slot, entry: JSONRecord | None = None) -> JSONRecord:
    return {
        "trial_id": slot.trial_id,
        "workload_id": slot.workload_id,
        "block": slot.block,
        "side": slot.side,
        "bundle_key": slot.bundle_key,
        "run_id": None if entry is None else entry["run"]["run_id"],
    }


def _stop(
    slot: Slot,
    entry: JSONRecord | None,
    reason: str,
    detail: str,
    evidence: StopEvidence | None = None,
) -> JSONRecord:
    evidence = evidence or StopEvidence()
    label = _entry_label(slot, entry)
    if evidence.run_id is not None:
        label["run_id"] = evidence.run_id
    stop = {
        **{key: value for key, value in label.items() if value is not None},
        "reason": reason,
        "detail": detail,
        "subsequent_run_ids": list(evidence.subsequent),
    }
    if evidence.error is not None:
        stop["error"] = evidence.error
    return stop


def _attempts(
    plan: JSONRecord, runs: list[JSONRecord]
) -> tuple[tuple[Slot, ...], dict[str, list[JSONRecord]]]:
    slots = comparison_slots(plan)
    by_trial = {}
    for entry in runs:
        by_trial.setdefault(entry["run"]["trial_id"], []).append(entry)
    return slots, by_trial


def _rejection_stop(
    slot: Slot,
    position: int,
    rejection: JSONRecord,
    slots: tuple[Slot, ...],
    by_trial: dict[str, list[JSONRecord]],
) -> JSONRecord:
    error = rejection.get("error")
    detail = rejection.get("detail") or (
        error.get("reason") if error is not None else rejection.get("reason")
    )
    underlying = error or {"reason": detail}
    return _stop(
        slot,
        None,
        HISTORY_IDENTITY_ERROR,
        detail,
        StopEvidence(
            subsequent=tuple(_later_run_ids(slots, position, by_trial)),
            run_id=rejection.get("run_id"),
            error=underlying,
        ),
    )


def _batch_rejection_stop(rejections: tuple[JSONRecord, ...] | list[JSONRecord]) -> JSONRecord | None:
    for rejection in rejections:
        if rejection.get("trial_id") is not None and rejection.get("reason") != UNPLANNED_TRIAL:
            continue
        detail = "rejection_outside_planned_slots"
        error = rejection.get("error") or {
            "reason": rejection.get("detail") or rejection.get("reason") or detail
        }
        stop = {
            "reason": HISTORY_IDENTITY_ERROR,
            "detail": detail,
            "subsequent_run_ids": [],
            "error": error,
        }
        for key in ("trial_id", "run_id"):
            if rejection.get(key) is not None:
                stop[key] = rejection[key]
        return stop
    return None


def _later_run_ids(
    slots: tuple[Slot, ...], position: int, by_trial: dict[str, list[JSONRecord]]
) -> list[str]:
    return [
        later["run"]["run_id"]
        for later_slot in slots[position + 1 :]
        for later in by_trial.get(later_slot.trial_id, [])
    ]


def comparison_history_stop(
    plan: JSONRecord,
    runs: list[JSONRecord],
    rejections: tuple[JSONRecord, ...] | list[JSONRecord] = (),
) -> JSONRecord | None:
    """Return the earliest comparison-specific stop in frozen slot order.

    Identity rejections and terminal run states are judged for one slot before
    looking at later slots, so a later gap cannot replace an earlier cancel. A
    missing slot with later evidence is itself the stop; rejected runs never
    enter handoff or bundle metrics.
    """
    slots, by_trial = _attempts(plan, runs)
    batch_stop = _batch_rejection_stop(rejections)
    rejected_by_trial = {}
    for rejection in rejections:
        trial_id = rejection.get("trial_id")
        if trial_id is not None:
            rejected_by_trial.setdefault(trial_id, []).append(rejection)
    for position, slot in enumerate(slots):
        rejected = rejected_by_trial.get(slot.trial_id, [])
        if rejected:
            return _rejection_stop(slot, position, rejected[0], slots, by_trial)
        attempts = by_trial.get(slot.trial_id, [])
        if attempts:
            if len(attempts) != 1:
                return _stop(
                    slot,
                    attempts[0],
                    HISTORY_IDENTITY_ERROR,
                    "duplicate_trial_attempt",
                    StopEvidence(subsequent=tuple(_later_run_ids(slots, position, by_trial))),
                )
            stop = _terminal_stop(slot, attempts[0])
            if stop is not None:
                stop["subsequent_run_ids"] = _later_run_ids(slots, position, by_trial)
                return stop
            continue
        if batch_stop is not None:
            return batch_stop
        later = _later_run_ids(slots, position, by_trial)
        if later:
            return _stop(
                slot,
                None,
                HISTORY_IDENTITY_ERROR,
                "subsequent_run_present",
                StopEvidence(subsequent=tuple(later)),
            )
    return batch_stop


def _execution_stop(slot: Slot, entry: JSONRecord) -> JSONRecord | None:
    summary = entry["summary"]
    states = [request["execution_state"] for request in entry["requests"]]
    if summary.get("stop_reason") == "user_cancelled" or "cancelled" in states:
        return _stop(slot, entry, HISTORY_CANCELLED, "user_cancelled")
    if summary.get("stop_reason") != "plan_finished" or any(
        state in ("failed", "invalid", "not_executed") for state in states
    ):
        detail = (
            summary.get("stop_reason")
            if summary.get("stop_reason") != "plan_finished"
            else "failed_request"
        )
        return _stop(slot, entry, HISTORY_FAILED, detail)
    return None


def _terminal_stop(slot: Slot, entry: JSONRecord) -> JSONRecord | None:
    stop = _execution_stop(slot, entry)
    if stop is not None:
        return stop
    summary = entry["summary"]
    states = [request["execution_state"] for request in entry["requests"]]
    if (
        any(state != "completed" for state in states)
        or not summary.get("scope_complete")
        or not summary.get("evidence_complete")
    ):
        return _stop(slot, entry, HISTORY_INCOMPLETE, "incomplete_success")
    return None
