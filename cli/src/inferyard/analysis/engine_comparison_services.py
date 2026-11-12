"""Service identity diagnostics for a selected comparison run set."""

from __future__ import annotations

from inferyard.contracts.comparison_types import JSONRecord
from inferyard.analysis.engine_comparison_shared import (
    SERVICE_IDENTITY_INVALID,
    SERVICE_IDENTITY_MISSING,
    SERVICE_NOT_REPLACED,
    append_diagnostic,
)
from inferyard.config.comparison_plan import Slot


def _service_identity(entry: JSONRecord) -> tuple[int, int] | None:
    try:
        service = entry["identity"]["local_engine"]["binding"]["service"]
    except (KeyError, TypeError):
        return None
    if type(service.get("pid")) is not int or type(service.get("start_ticks")) is not int:
        return None
    return service["pid"], service["start_ticks"]


def service_diagnostics(
    slots: tuple[Slot, ...], selected: dict[str, JSONRecord], diagnostics: list[JSONRecord]
) -> None:
    seen = {}
    for slot in slots:
        entry = selected.get(slot.trial_id)
        if entry is None:
            continue
        service = _service_identity(entry)
        if service is None:
            raw = entry.get("identity")
            reason = SERVICE_IDENTITY_MISSING if raw is None else SERVICE_IDENTITY_INVALID
            append_diagnostic(
                diagnostics,
                reason,
                trial_id=slot.trial_id,
                run_id=entry["run"]["run_id"],
                workload_id=slot.workload_id,
            )
            continue
        if service in seen:
            append_diagnostic(
                diagnostics,
                SERVICE_NOT_REPLACED,
                trial_id=slot.trial_id,
                run_id=entry["run"]["run_id"],
                detail=f"same_process_as:{seen[service]}",
            )
        else:
            seen[service] = slot.trial_id
