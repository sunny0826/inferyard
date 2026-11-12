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

from inferyard.analysis.comparison_history import comparison_history_stop
from inferyard.analysis.engine_comparison_runset import (
    DUPLICATE_ATTEMPT,
    HANDOFF_BROKEN,
    HANDOFF_MISSING,
    HANDOFF_SLOT_MISMATCH,
    PLAN_HASH_MISMATCH,
    PLANNED_RUN_MISSING,
    RUN_PLAN_MISMATCH,
    SERVICE_IDENTITY_INVALID,
    SERVICE_IDENTITY_MISSING,
    SERVICE_NOT_REPLACED,
    UNPLANNED_TRIAL,
    append_diagnostic,
    build_run_set,
)
from inferyard.config.comparison_plan import comparison_slots, is_comparison_plan
from inferyard.contracts.comparison_types import JSONRecord
from inferyard.errors import ContractError, EvidenceError

DEFINITION = "engine-comparison.v2"
PLAN_REQUIRED = "comparison_plan_required"

# Retired/duplicate names are intentionally absent: comparison_unplanned_trial
# covers unplanned_run_present, and actual continuity is proven by the handoff
# chain, so execution_order_differs is never emitted.
__all__ = [
    "DEFINITION",
    "DUPLICATE_ATTEMPT",
    "HANDOFF_BROKEN",
    "HANDOFF_MISSING",
    "HANDOFF_SLOT_MISMATCH",
    "PLANNED_RUN_MISSING",
    "RUN_PLAN_MISMATCH",
    "SERVICE_IDENTITY_INVALID",
    "SERVICE_IDENTITY_MISSING",
    "SERVICE_NOT_REPLACED",
    "UNPLANNED_TRIAL",
    "bundle_comparisons",
    "comparison_history_stop",
    "empty_comparison_document",
    "evaluate_comparison",
    "is_comparison_plan",
]


def empty_comparison_document(
    plan: JSONRecord | None,
    plan_path: Path | str,
    issue: JSONRecord,
    *,
    plan_hash_ok: bool = True,
) -> JSONRecord:
    document = {
        "definition": DEFINITION,
        "plan": {
            "path": str(plan_path),
            "sha256": None if plan is None else plan.get("plan_sha256"),
        },
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
    append_diagnostic(
        document["diagnostics"],
        issue["reason"],
        detail=issue["detail"],
        error=issue.get("error"),
    )
    if not plan_hash_ok:
        append_diagnostic(document["diagnostics"], PLAN_HASH_MISMATCH, detail="batch_plan_mismatch")
    return document


def evaluate_comparison(
    plan: JSONRecord,
    runs: list[JSONRecord],
    *,
    plan_path: Path | str | None = None,
    plan_hash_ok: bool = True,
    evidence_errors: tuple[JSONRecord, ...] | list[JSONRecord] = (),
) -> JSONRecord:
    """Build the v2 run set, handoff chain, history stop and bundle slots."""
    try:
        slots = comparison_slots(plan)
    except (ContractError, KeyError, TypeError, ValueError) as exc:
        return empty_comparison_document(
            plan,
            plan_path,
            {"reason": PLAN_REQUIRED, "detail": str(exc)},
            plan_hash_ok=plan_hash_ok,
        )
    parts = build_run_set(plan, slots, runs, evidence_errors, plan_hash_ok=plan_hash_ok)
    return {
        "definition": DEFINITION,
        "plan": {"path": str(plan_path), "sha256": plan["plan_sha256"]},
        **parts,
        "eligible": not parts["diagnostics"],
    }


def _blocked_comparison(document: JSONRecord) -> JSONRecord:
    from inferyard.analysis.engine_blocks import METRIC_IDS, METRICS

    return {
        "definition": "engine-comparison.v1",
        "eligible": False,
        "blockers": sorted(
            {row["reason"] for row in document["diagnostics"]} | {"comparison_overall_blocked"}
        ),
        "overall_diagnostics": document["diagnostics"],
        "blocks": [],
        "verdicts": {
            metric: {
                "metric_id": METRIC_IDS[metric],
                "higher_is_better": higher,
                "verdict": "not_eligible",
            }
            for metric, higher in METRICS.items()
        },
    }


def _bundle_result(
    group: JSONRecord, data_by_run_id: dict[str, JSONRecord]
) -> JSONRecord:
    from inferyard.analysis.engine_blocks import compare_paired_blocks

    ordered = []
    for block in group["blocks"]:
        for identifier in (block["left_run_id"], block["right_run_id"]):
            if identifier is None or identifier not in data_by_run_id:
                raise EvidenceError("comparison_bundle_run_missing")
            ordered.append(data_by_run_id[identifier])
    return compare_paired_blocks(ordered)


def bundle_comparisons(
    document: JSONRecord, data_by_run_id: dict[str, JSONRecord]
) -> JSONRecord:
    """Judge each bundle only for a fully eligible whole-plan run set."""
    if not document["eligible"]:
        return {key: _blocked_comparison(document) for key in document["bundles"]}
    return {
        key: _bundle_result(group, data_by_run_id)
        for key, group in document["bundles"].items()
    }
