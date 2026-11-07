"""Comparison-specific history stop shared by execution and v2 derivation (ADR 057c).

The stop reads only frozen slot order. It never treats a normal handoff as a run
terminal state, and model answer quality is outside execution stopping.
"""

from __future__ import annotations

from inferyard.config.comparison_plan import Slot, comparison_slots

HISTORY_IDENTITY_ERROR = "comparison_history_identity_error"
HISTORY_CANCELLED = "comparison_history_cancelled"
HISTORY_FAILED = "comparison_history_failed"
HISTORY_INCOMPLETE = "comparison_history_incomplete"


def _entry_label(slot: Slot, entry: dict | None = None) -> dict:
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
    entry: dict | None,
    reason: str,
    detail: str,
    *,
    subsequent: tuple[str, ...] = (),
) -> dict:
    return {
        **{key: value for key, value in _entry_label(slot, entry).items() if value is not None},
        "reason": reason,
        "detail": detail,
        "subsequent_run_ids": list(subsequent),
    }


def _attempts(plan: dict, runs: list[dict]) -> tuple[tuple[Slot, ...], dict[str, list[dict]]]:
    slots = comparison_slots(plan)
    by_trial = {}
    for entry in runs:
        by_trial.setdefault(entry["run"]["trial_id"], []).append(entry)
    return slots, by_trial


def _gap_with_later_run(
    slots: tuple[Slot, ...], by_trial: dict[str, list[dict]], runs: list[dict]
) -> dict | None:
    index = {slot.trial_id: position for position, slot in enumerate(slots)}
    first_missing = next(
        (position for position, slot in enumerate(slots) if not by_trial.get(slot.trial_id)),
        None,
    )
    if first_missing is None:
        return None
    later = [entry for entry in runs if index.get(entry["run"]["trial_id"], -1) > first_missing]
    if not later:
        return None
    return _stop(slots[first_missing], later[0], HISTORY_IDENTITY_ERROR, "subsequent_run_present")


def _later_run_ids(
    slots: tuple[Slot, ...], position: int, by_trial: dict[str, list[dict]]
) -> list[str]:
    return [
        later["run"]["run_id"]
        for later_slot in slots[position + 1 :]
        for later in by_trial.get(later_slot.trial_id, [])
    ]


def comparison_history_stop(plan: dict, runs: list[dict]) -> dict | None:
    """Return the first comparison-specific history stop, or ``None``.

    Only frozen slot order is consulted. A later run after a missing slot, a
    resumed/duplicated trial, a cancelled slot, an execution failure or an
    incomplete success blocks further dispatch.
    """
    slots, by_trial = _attempts(plan, runs)
    gap = _gap_with_later_run(slots, by_trial, runs)
    if gap is not None:
        return gap
    for position, slot in enumerate(slots):
        attempts = by_trial.get(slot.trial_id, [])
        if not attempts:
            continue
        if len(attempts) != 1:
            return _stop(slot, attempts[0], HISTORY_IDENTITY_ERROR, "duplicate_trial_attempt")
        stop = _terminal_stop(slot, attempts[0])
        if stop is not None:
            stop["subsequent_run_ids"] = _later_run_ids(slots, position, by_trial)
            return stop
    return None


def _execution_stop(slot: Slot, entry: dict) -> dict | None:
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


def _terminal_stop(slot: Slot, entry: dict) -> dict | None:
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
