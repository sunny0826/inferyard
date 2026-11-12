"""Read a frozen comparison plan and batch into the internal v2 run set.

The reader validates the whole batch first and selects runs by frozen slot order.
"""

from __future__ import annotations

from pathlib import Path

from inferyard.analysis.comparison_history import HISTORY_IDENTITY_ERROR
from inferyard.analysis.engine_comparison_runset import PLAN_HASH_MISMATCH, append_diagnostic
from inferyard.analysis.engine_comparison_v2 import (
    PLAN_REQUIRED,
    empty_comparison_document,
    evaluate_comparison,
)
from inferyard.config.comparison_plan import validate_comparison_plan
from inferyard.config.loader import LoadedConfig
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.contracts.batch_envelope import validate_batch_envelope
from inferyard.contracts.comparison_types import JSONRecord
from inferyard.errors import ContractError, EvidenceError, UnsupportedFormat
from inferyard.evidence.storage import read_json
from inferyard.runtime.batch_state import history_diagnostics


def _read_plan(
    plan_path: Path,
) -> tuple[JSONRecord | None, dict[str, LoadedConfig] | None, JSONRecord | None]:
    try:
        plan, loaded = read_frozen_plan(plan_path)
    except (EvidenceError, ContractError, OSError, ValueError, TypeError, KeyError, IndexError) as exc:
        return None, None, empty_comparison_document(
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
        return plan, None, empty_comparison_document(
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
        return True, empty_comparison_document(
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
        return plan_hash_ok, empty_comparison_document(
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
        return None, None, empty_comparison_document(
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
