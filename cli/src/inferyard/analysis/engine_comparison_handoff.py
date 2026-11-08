"""Handoff chain diagnostics for a selected comparison run set."""

from __future__ import annotations

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.analysis.engine_comparison_shared import (
    HANDOFF_BROKEN,
    HANDOFF_MISSING,
    HANDOFF_SLOT_MISMATCH,
    append_diagnostic,
    slot_label,
)
from inferyard.config.comparison_plan import Slot


def _handoff_outcome(
    position: int,
    entry: JSONRecord | None,
    slots: tuple[Slot, ...],
    selected: dict[str, JSONRecord],
) -> tuple[str, str | None, int | None]:
    if entry is None:
        return "not_selected", None, None
    binding = entry.get("service_binding")
    if binding is None:
        return HANDOFF_MISSING, "sealed_service_binding_absent", None
    if "previous_run_id" not in binding:
        return HANDOFF_MISSING, "previous_run_id_absent", None
    previous = binding["previous_run_id"]
    if position == 0:
        if previous is not None:
            return HANDOFF_SLOT_MISMATCH, "first_slot_has_predecessor", previous
        return "ok", None, previous
    predecessor = selected.get(slots[position - 1].trial_id)
    if predecessor is None:
        return "not_checked", None, previous
    if previous is None:
        return HANDOFF_MISSING, "expected_predecessor_absent", previous
    if previous != predecessor["run"]["run_id"]:
        return HANDOFF_BROKEN, "predecessor_is_not_previous_slot", previous
    return "ok", None, previous


def handoff_rows(
    slots: tuple[Slot, ...], selected: dict[str, JSONRecord], diagnostics: list[JSONRecord]
) -> list[JSONRecord]:
    rows = []
    for position, slot in enumerate(slots):
        entry = selected.get(slot.trial_id)
        status, reason, previous = _handoff_outcome(position, entry, slots, selected)
        row = {
            **slot_label(slot),
            "run_id": None if entry is None else entry["run"]["run_id"],
            "previous_run_id": previous,
            "status": status,
        }
        if reason is not None:
            append_diagnostic(
                diagnostics,
                status,
                trial_id=slot.trial_id,
                run_id=row["run_id"],
                detail=reason,
            )
        rows.append(row)
    return rows
