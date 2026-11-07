"""Shared diagnostics and slot labels for comparison run-set derivation."""

from __future__ import annotations

from inferyard.analysis.comparison_types import JSONRecord, JSONValue
from inferyard.config.comparison_plan import Slot

PLAN_HASH_MISMATCH = "comparison_plan_hash_mismatch"
PLANNED_RUN_MISSING = "planned_run_missing"
HANDOFF_MISSING = "comparison_handoff_missing"
HANDOFF_BROKEN = "comparison_handoff_broken"
HANDOFF_SLOT_MISMATCH = "comparison_handoff_slot_mismatch"
SERVICE_IDENTITY_MISSING = "comparison_service_identity_missing"
SERVICE_IDENTITY_INVALID = "comparison_service_identity_invalid"
SERVICE_NOT_REPLACED = "comparison_service_not_replaced"


def append_diagnostic(
    diagnostics: list[JSONRecord], reason: str, **fields: JSONValue
) -> None:
    row = {"reason": reason, **{key: value for key, value in fields.items() if value is not None}}
    if row not in diagnostics:
        diagnostics.append(row)


def slot_label(slot: Slot) -> JSONRecord:
    return {
        "trial_id": slot.trial_id,
        "workload_id": slot.workload_id,
        "block": slot.block,
        "side": slot.side,
        "bundle_key": slot.bundle_key,
    }
