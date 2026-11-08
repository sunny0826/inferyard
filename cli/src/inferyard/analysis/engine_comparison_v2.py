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

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.analysis.comparison_history import (
    HISTORY_IDENTITY_ERROR,
    comparison_history_stop,
)
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
from inferyard.config.comparison_plan import (
    Slot,
    comparison_slots,
    is_comparison_plan,
    validate_comparison_plan,
)
from inferyard.config.loader import LoadedConfig
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.contracts.validation import ContractError
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.storage import EvidenceError, read_json
from inferyard.runtime.batch_envelope import validate_batch_envelope
from inferyard.runtime.batch_state import history_diagnostics

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
    "derive_comparison_v2",
    "evaluate_comparison",
    "is_comparison_plan",
    "read_comparison_v2",
]


def _empty(
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


def _read_plan(
    plan_path: Path,
) -> tuple[JSONRecord | None, dict[str, LoadedConfig] | None, JSONRecord | None]:
    try:
        plan, loaded = read_frozen_plan(plan_path)
    except (EvidenceError, ContractError, OSError, ValueError, TypeError, KeyError, IndexError) as exc:
        return None, None, _empty(
            None,
            plan_path,
            {
                "reason": PLAN_REQUIRED,
                "detail": "plan_unreadable",
                "error": {"type": type(exc).__name__, "reason": str(exc)},
            },
        )
    try:
        validate_comparison_plan(plan, loaded)
    except (ContractError, KeyError, TypeError, ValueError) as exc:
        return plan, None, _empty(
            plan,
            plan_path,
            {
                "reason": PLAN_REQUIRED,
                "detail": str(exc),
                "error": {"type": type(exc).__name__, "reason": str(exc)},
            },
        )
    return plan, loaded, None


def _read_batch(root: Path, plan: JSONRecord, plan_path: Path) -> tuple[bool, JSONRecord | None]:
    try:
        metadata = read_json(root / "batch.json")
        validate_batch_envelope(metadata)
    except (
        EvidenceError,
        ContractError,
        UnsupportedFormat,
        OSError,
        ValueError,
        TypeError,
        KeyError,
        IndexError,
    ) as exc:
        return True, _empty(
            plan,
            plan_path,
            {
                "reason": HISTORY_IDENTITY_ERROR,
                "detail": "batch_envelope_invalid",
                "error": {"reason": str(exc) or type(exc).__name__},
            },
        )
    plan_hash_ok = metadata["plan_sha256"] == plan["plan_sha256"]
    runs_root = root / "runs"
    if not runs_root.is_dir() or runs_root.is_symlink():
        return plan_hash_ok, _empty(
            plan,
            plan_path,
            {"reason": HISTORY_IDENTITY_ERROR, "detail": "batch_runs_invalid"},
            plan_hash_ok=plan_hash_ok,
        )
    return plan_hash_ok, None


def _read_history(
    root: Path,
    plan: JSONRecord,
    loaded: dict[str, LoadedConfig],
    plan_path: Path,
    plan_hash_ok: bool,
) -> tuple[list[JSONRecord] | None, list[JSONRecord] | None, JSONRecord | None]:
    try:
        runs, errors = history_diagnostics(root, plan, loaded)
    except (EvidenceError, ContractError, OSError, ValueError, TypeError) as exc:
        return None, None, _empty(
            plan,
            plan_path,
            {
                "reason": HISTORY_IDENTITY_ERROR,
                "detail": "batch_history_unreadable",
                "error": {"type": type(exc).__name__, "reason": str(exc)},
            },
            plan_hash_ok=plan_hash_ok,
        )
    return runs, errors, None


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
        return _empty(
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


def _read_comparison(
    plan_path: Path | str, batch_root: Path | str
) -> tuple[JSONRecord, list[JSONRecord]]:
    path = Path(plan_path)
    plan_path = path / "plan.json" if path.is_dir() else path
    root = Path(batch_root).resolve()
    plan, loaded, failure = _read_plan(plan_path)
    if failure is not None:
        return failure, []
    plan_hash_ok, failure = _read_batch(root, plan, plan_path)
    if failure is not None:
        return failure, []
    runs, errors, failure = _read_history(root, plan, loaded, plan_path, plan_hash_ok)
    if failure is not None:
        return failure, []
    return (
        evaluate_comparison(
            plan,
            runs,
            plan_path=plan_path,
            plan_hash_ok=plan_hash_ok,
            evidence_errors=errors,
        ),
        runs,
    )


def read_comparison_v2(plan_path: Path | str, batch_root: Path | str) -> JSONRecord:
    """Read one batch into the internal v2 document without computing metrics."""
    return _read_comparison(plan_path, batch_root)[0]


def derive_comparison_v2(
    plan_path: Path | str, batch_root: Path | str
) -> tuple[JSONRecord, list[JSONRecord]]:
    """Read one batch and retain the verified run data used for v2 metrics."""
    return _read_comparison(plan_path, batch_root)


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
