"""Internal engine-comparison.v2 run-set construction (ADR 057c).

One CLI judgement source serves both the execution entry (before dispatching the
next frozen slot) and this offline reader. The reader validates the whole batch
first, selects runs by frozen slot order, and only then exposes per-bundle block
groups for the existing six-block verdict. It never sorts slots by event time.

T2a keeps this internal: ``compare-engines`` still derives v1. Public v2 output,
HTML and ``verify`` belong to T2b.
"""

from __future__ import annotations

from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.analysis.comparison_history import (
    HISTORY_IDENTITY_ERROR,
    comparison_history_stop,
)
from inferyard.config.comparison_plan import (
    comparison_slots,
    is_comparison_plan,
    validate_comparison_plan,
)
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, read_json
from inferyard.implementation_identity import execution_matches

DEFINITION = "engine-comparison.v2"

PLAN_REQUIRED = "comparison_plan_required"
PLAN_HASH_MISMATCH = "comparison_plan_hash_mismatch"
PLANNED_RUN_MISSING = "planned_run_missing"
UNPLANNED_TRIAL = "comparison_unplanned_trial"
RUN_PLAN_MISMATCH = "run_plan_mismatch"
DUPLICATE_ATTEMPT = "comparison_duplicate_trial_attempt"
HANDOFF_MISSING = "comparison_handoff_missing"
HANDOFF_BROKEN = "comparison_handoff_broken"
HANDOFF_SLOT_MISMATCH = "comparison_handoff_slot_mismatch"

# Retired/duplicate names are intentionally absent: comparison_unplanned_trial
# covers unplanned_run_present, and actual continuity is proven by the handoff
# chain, so execution_order_differs is never emitted.
__all__ = [
    "DEFINITION",
    "comparison_history_stop",
    "evaluate_comparison",
    "is_comparison_plan",
    "read_comparison_v2",
]


def _append(diagnostics, reason, **fields):
    row = {"reason": reason, **{key: value for key, value in fields.items() if value is not None}}
    if row not in diagnostics:
        diagnostics.append(row)


def _empty(plan, plan_path, reason, detail, *, plan_hash_ok=True):
    document = {
        "definition": DEFINITION,
        "plan": {"path": str(plan_path), "sha256": plan.get("plan_sha256")},
        "run_set": {
            "expected_trials": [],
            "observed_runs": [],
            "missing": [],
            "unplanned": [],
            "mismatched": [],
            "duplicates": [],
        },
        "handoff": {"slots": [], "blockers": []},
        "history_stop": None,
        "bundles": {},
        "diagnostics": [],
        "eligible": False,
    }
    _append(document["diagnostics"], reason, detail=detail)
    if not plan_hash_ok:
        _append(document["diagnostics"], PLAN_HASH_MISMATCH, detail="batch_plan_mismatch")
    return document


def _plan_mismatch_reason(entry, plan):
    run, experiment = entry["run"], plan["experiment"]
    if (
        run["plan_sha256"] != plan["plan_sha256"]
        or run["experiment_id"] != experiment["experiment_id"]
        or run["definition_versions"] != experiment["definition_versions"]
    ):
        return RUN_PLAN_MISMATCH
    return None


def _slot_label(slot):
    return {
        "trial_id": slot.trial_id,
        "workload_id": slot.workload_id,
        "block": slot.block,
        "side": slot.side,
        "bundle_key": slot.bundle_key,
    }


def _observed_label(run, entry, slot):
    return {
        "trial_id": run["trial_id"],
        "run_id": run["run_id"],
        "path": str(entry.get("path")),
        "workload_id": None if slot is None else slot.workload_id,
        "block": None if slot is None else slot.block,
        "side": None if slot is None else slot.side,
        "bundle_key": None if slot is None else slot.bundle_key,
    }


def _classify_entry(plan, slots, index, entry, state):
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
        return
    if position is None:
        run_set["unplanned"].append(
            {"trial_id": run["trial_id"], "run_id": run["run_id"], "path": str(entry.get("path"))}
        )
        return
    if run["run_id"] in seen:
        run_set["duplicates"].append(
            {
                "trial_id": run["trial_id"],
                "run_id": run["run_id"],
                "path": str(entry.get("path")),
                "detail": "duplicate_run_id",
            }
        )
        rejected.add(run["trial_id"])
        return
    seen.add(run["run_id"])
    reason = _plan_mismatch_reason(entry, plan)
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
    by_trial.setdefault(run["trial_id"], []).append(entry)


def _classify_runs(plan, slots, runs):
    run_set = {
        "expected_trials": [slot.trial_id for slot in slots],
        "observed_runs": [],
        "missing": [],
        "unplanned": [],
        "mismatched": [],
        "duplicates": [],
    }
    index = {slot.trial_id: position for position, slot in enumerate(slots)}
    by_trial, seen, rejected = {}, set(), set()
    state = {"run_set": run_set, "by_trial": by_trial, "seen": seen, "rejected": rejected}
    for entry in runs:
        _classify_entry(plan, slots, index, entry, state)
    return run_set, by_trial, rejected


def _select_slots(run_set, slots, by_trial, rejected):
    selected = {}
    for slot in slots:
        attempts = by_trial.get(slot.trial_id, [])
        if not attempts:
            if slot.trial_id not in rejected:
                run_set["missing"].append(_slot_label(slot))
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


def _run_set_diagnostics(run_set, diagnostics):
    for row in run_set["missing"]:
        _append(
            diagnostics,
            PLANNED_RUN_MISSING,
            trial_id=row["trial_id"],
            workload_id=row["workload_id"],
        )
    for row in run_set["unplanned"]:
        _append(
            diagnostics,
            UNPLANNED_TRIAL,
            trial_id=row["trial_id"],
            run_id=row["run_id"],
            detail="unplanned_run_present",
        )
    for row in run_set["mismatched"]:
        _append(
            diagnostics,
            RUN_PLAN_MISMATCH if row["detail"] == RUN_PLAN_MISMATCH else HISTORY_IDENTITY_ERROR,
            trial_id=row["trial_id"],
            run_id=row["run_id"],
            detail=row["detail"],
            error=row.get("error"),
        )
    for row in run_set["duplicates"]:
        _append(
            diagnostics,
            DUPLICATE_ATTEMPT,
            trial_id=row["trial_id"],
            run_id=row.get("run_id"),
            detail=row["detail"],
        )


def _handoff_outcome(position, entry, slots, selected):
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


def _handoff(slots, selected, diagnostics):
    rows = []
    for position, slot in enumerate(slots):
        entry = selected.get(slot.trial_id)
        status, reason, previous = _handoff_outcome(position, entry, slots, selected)
        row = {
            **_slot_label(slot),
            "run_id": None if entry is None else entry["run"]["run_id"],
            "previous_run_id": previous,
            "status": status,
        }
        if reason is not None:
            _append(
                diagnostics,
                status,
                trial_id=slot.trial_id,
                run_id=row["run_id"],
                detail=reason,
            )
        rows.append(row)
    return rows


def _bundle_headers(plan, slots):
    workloads = {item["workload_id"]: item for item in plan["experiment"]["workloads"]}
    groups = {}
    for slot in slots:
        if slot.bundle_key not in groups:
            groups[slot.bundle_key] = {
                "bundle_sha256": workloads[slot.workload_id]["bundle"]["sha256"],
                "left": [],
                "right": [],
                "blocks": [],
            }
    return groups


def _append_side_runs(groups, slots, selected):
    block_count = max(slot.block for slot in slots)
    for block in range(1, block_count + 1):
        for slot in slots:
            entry = selected.get(slot.trial_id)
            if slot.block != block or entry is None:
                continue
            side = "left" if slot.side == "a" else "right"
            groups[slot.bundle_key][side].append(entry["run"]["run_id"])
    return block_count


def _append_bundle_pairs(groups, slots, selected, block_count):
    for key, group in groups.items():
        for pair in range(1, block_count // 2 + 1):
            left_block, right_block = pair * 2 - 1, pair * 2
            group["blocks"].append(
                {
                    "pair": pair,
                    "left_block": left_block,
                    "right_block": right_block,
                    "left_run_id": _block_run(selected, slots, key, left_block),
                    "right_run_id": _block_run(selected, slots, key, right_block),
                }
            )


def _bundle_groups(plan, slots, selected):
    groups = _bundle_headers(plan, slots)
    block_count = _append_side_runs(groups, slots, selected)
    _append_bundle_pairs(groups, slots, selected, block_count)
    return groups


def _block_run(selected, slots, key, block):
    entry = next(
        (
            selected[slot.trial_id]
            for slot in slots
            if slot.bundle_key == key and slot.block == block and slot.trial_id in selected
        ),
        None,
    )
    return None if entry is None else entry["run"]["run_id"]


def evaluate_comparison(plan, runs, *, plan_path=None, plan_hash_ok=True, evidence_errors=()):
    """Build the v2 run set, handoff chain, history stop and bundle slots."""
    try:
        slots = comparison_slots(plan)
    except (ContractError, KeyError, TypeError, ValueError) as exc:
        return _empty(plan, plan_path, PLAN_REQUIRED, str(exc), plan_hash_ok=plan_hash_ok)
    diagnostics = []
    if not plan_hash_ok:
        _append(diagnostics, PLAN_HASH_MISMATCH, detail="batch_plan_mismatch")
    for error in evidence_errors:
        _append(
            diagnostics,
            HISTORY_IDENTITY_ERROR,
            trial_id=error.get("trial_id"),
            run_id=error.get("run_id"),
            path=error.get("path"),
            detail=error.get("detail"),
            error=error.get("error"),
        )
    run_set, by_trial, rejected = _classify_runs(plan, slots, runs)
    selected = _select_slots(run_set, slots, by_trial, rejected)
    _run_set_diagnostics(run_set, diagnostics)
    handoff_rows = _handoff(slots, selected, diagnostics)
    history_stop = None
    if not diagnostics:
        history_stop = comparison_history_stop(plan, runs)
        if history_stop is not None:
            _append(
                diagnostics,
                history_stop["reason"],
                trial_id=history_stop.get("trial_id"),
                run_id=history_stop.get("run_id"),
                detail=history_stop.get("detail"),
            )
    return {
        "definition": DEFINITION,
        "plan": {"path": str(plan_path), "sha256": plan["plan_sha256"]},
        "run_set": run_set,
        "handoff": {
            "slots": handoff_rows,
            "blockers": [
                row
                for row in handoff_rows
                if row["status"] not in ("ok", "not_checked", "not_selected")
            ],
        },
        "history_stop": history_stop,
        "bundles": _bundle_groups(plan, slots, selected),
        "diagnostics": diagnostics,
        "eligible": not diagnostics,
    }


def _batch_identity_error(entry, metadata):
    run = entry["run"]
    if "implementation_identity" in metadata:
        if not execution_matches(
            run.get("implementation_identity"), metadata["implementation_identity"]
        ):
            return "batch_run_tool_changed"
    elif run.get("tool_source_sha256") != metadata.get("tool_source_sha256"):
        return "batch_run_tool_changed"
    if run.get("diagnostic") != metadata.get("diagnostic"):
        return "batch_diagnostic_mode_changed"
    return None


def _binding(metadata):
    evidence = metadata["evidence"]
    if metadata["manifest"] is None:
        return None
    binding = evidence.observation("service_binding", stage="prepare")
    if binding is None:
        return None
    reference = evidence.observation_reference("service_binding", stage="prepare")
    if reference["path"] not in metadata["manifest"]["files"]:
        return None
    return binding


def _read_runs(root, metadata):
    runs, evidence_errors = [], []
    for path in sorted((root / "runs").iterdir()):
        if not path.is_dir() or path.is_symlink():
            evidence_errors.append({"path": str(path), "detail": "unexpected_batch_entry"})
            continue
        trial_metadata = {}
        try:
            entry = read_trial(path, metadata=trial_metadata)
        except (
            EvidenceError,
            ContractError,
            UnsupportedFormat,
            OSError,
            ValueError,
            KeyError,
            TypeError,
        ) as exc:
            evidence_errors.append(
                {
                    "path": str(path),
                    "detail": "evidence_read_failed",
                    "error": {"type": type(exc).__name__, "reason": str(exc)},
                }
            )
            continue
        entry["path"] = path
        entry["service_binding"] = _binding(trial_metadata)
        identity_error = _batch_identity_error(entry, metadata)
        if identity_error is not None:
            entry["identity_error"] = identity_error
        runs.append(entry)
    return runs, evidence_errors


def read_comparison_v2(plan_path, batch_root):
    """Read one batch into the internal v2 document without computing metrics."""
    path = Path(plan_path)
    plan_path = path / "plan.json" if path.is_dir() else path
    root = Path(batch_root).resolve()
    plan, loaded = read_frozen_plan(plan_path)
    try:
        validate_comparison_plan(plan, loaded)
    except (ContractError, KeyError, TypeError, ValueError) as exc:
        return _empty(plan, plan_path, PLAN_REQUIRED, str(exc))
    metadata = read_json(root / "batch.json")
    plan_hash_ok = (
        metadata.get("schema_version") == SCHEMA_VERSION
        and metadata.get("plan_sha256") == plan["plan_sha256"]
    )
    runs, evidence_errors = _read_runs(root, metadata)
    return evaluate_comparison(
        plan,
        runs,
        plan_path=plan_path,
        plan_hash_ok=plan_hash_ok,
        evidence_errors=evidence_errors,
    )


def bundle_comparisons(document, data_by_run_id):
    """Apply the existing six-block verdict once per bundle, never to all 12 runs."""
    from inferyard.analysis.engine_blocks import compare_blocks

    results = {}
    for key, group in document["bundles"].items():
        run_ids = [*group["left"], *group["right"]]
        if any(identifier is None or identifier not in data_by_run_id for identifier in run_ids):
            raise EvidenceError("comparison_bundle_run_missing")
        results[key] = compare_blocks([data_by_run_id[identifier] for identifier in run_ids])
    return results
