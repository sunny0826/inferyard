"""Frozen batch history validation and diagnostic rejection collection."""

from __future__ import annotations

from collections.abc import Callable
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path

from inferyard.analysis.comparison_history import (
    DUPLICATE_ATTEMPT,
    HISTORY_IDENTITY_ERROR,
    REJECTION_CLASSIFICATIONS,
    RUN_PLAN_MISMATCH,
    UNPLANNED_TRIAL,
)
from inferyard.analysis.comparison_types import JSONRecord
from inferyard.config.loader import LoadedConfig
from inferyard.contracts.engine_profiles import is_local
from inferyard.contracts.validation import validate_document
from inferyard.errors import ContractError, EvidenceError, UnsupportedFormat
from inferyard.evidence.ledger import read_run_projection, read_trial
from inferyard.evidence.storage import read_json
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference
from inferyard.implementation_identity import execution_matches
from inferyard.provenance import tool_source_hash
from inferyard.runtime.batch_envelope import validate_batch_envelope


def _args_without_endpoint(args: list[str]) -> list[str]:
    result, index = [], 0
    while index < len(args):
        value = args[index]
        if value in ("--host", "--port", "-p"):
            index += 2
            continue
        if not value.startswith(("--host=", "--port=", "-p=")):
            result.append(value)
        index += 1
    return result


def immutable_config(config: JSONRecord) -> JSONRecord:
    value = deepcopy(config)
    value.pop("endpoint")
    value["engine"]["startup_args"] = _args_without_endpoint(value["engine"]["startup_args"])
    value["bundle"].pop("path")
    return value


def _history_scope(
    root: Path, allow_tool_change: bool, source_identity: str | None
) -> tuple[JSONRecord, str]:
    metadata = read_json(root / "batch.json")
    validate_batch_envelope(metadata)
    expected_tool = (
        metadata.get("tool_source_sha256")
        if allow_tool_change
        else source_identity or tool_source_hash()
    )
    return metadata, expected_tool


@dataclass(frozen=True, slots=True)
class HistoryContext:
    root: Path
    plan: JSONRecord
    loaded: dict[str, LoadedConfig]
    metadata: JSONRecord
    expected_tool: str
    allow_tool_change: bool
    implementation_identity: JSONRecord | None
    full_verification: bool
    retain_binding: bool


@dataclass(frozen=True, slots=True)
class HistoryOptions:
    source_identity: str | None = None
    implementation_identity: JSONRecord | None = None
    full_verification: bool = True


@dataclass(frozen=True, slots=True)
class RunRead:
    path: Path
    data: JSONRecord | None = None
    code: str | None = None
    error: Exception | None = None
    trial_id: str | None = None
    run_id: str | None = None
    classification: str | None = None
    detail: str | None = None


def _rejected(
    path: Path,
    code: str,
    run: JSONRecord | None = None,
    *,
    classification: str | None = None,
    detail: str | None = None,
) -> RunRead:
    return RunRead(
        path,
        code=code,
        trial_id=None if run is None else run.get("trial_id"),
        run_id=None if run is None else run.get("run_id"),
        classification=classification or code,
        detail=detail or code,
    )


def _identity_classification(
    run: JSONRecord, plan: JSONRecord
) -> tuple[str | None, str | None]:
    if run["trial_id"] not in {trial["trial_id"] for trial in plan["trials"]}:
        return UNPLANNED_TRIAL, None
    experiment = plan["experiment"]
    if (
        run["plan_sha256"] != plan["plan_sha256"]
        or run["experiment_id"] != experiment["experiment_id"]
        or run["definition_versions"] != experiment["definition_versions"]
    ):
        return RUN_PLAN_MISMATCH, RUN_PLAN_MISMATCH
    return None, None


def _read_envelope_identity(path: Path) -> JSONRecord:
    reads = TrialEvidence(path.resolve(), projection=True)
    if reads.layout_version != 2:
        raise EvidenceError("identity_fallback_requires_layout2")
    return {"run": reads.input("run"), "selection": reads.input("selection")}


def _failure_identity(path: Path, plan: JSONRecord) -> RunRead | None:
    try:
        identity = _read_envelope_identity(path)
    except (EvidenceError, ContractError, UnsupportedFormat, OSError, ValueError, KeyError, TypeError, IndexError):
        return None
    run = identity["run"]
    try:
        classification, detail = _identity_classification(run, plan)
    except (KeyError, TypeError):
        return None
    return _rejected(
        path,
        classification or HISTORY_IDENTITY_ERROR,
        run,
        classification=classification or HISTORY_IDENTITY_ERROR,
        detail=detail,
    )


def _validate_run_documents(data: JSONRecord) -> None:
    try:
        validate_document("run", data["run"])
        validate_document("selection", data["selection"])
    except ContractError as exc:
        raise EvidenceError("invalid_trial_input") from exc


def _load_run(path: Path, context: HistoryContext) -> tuple[JSONRecord, JSONRecord]:
    trial_metadata = {}
    if context.full_verification:
        data = read_trial(path, metadata=trial_metadata)
        data["manifest_sealed"] = trial_metadata["manifest"] is not None
        data["service_drain"] = trial_metadata.get("service_drain")
        if is_local(data["config"]):
            data["identity"] = trial_metadata["identity"]
    else:
        data = read_run_projection(path)
        data["manifest_sealed"] = data.get("manifest_sealed", False)
    _validate_run_documents(data)
    return data, trial_metadata


def _identity_code(data: JSONRecord, context: HistoryContext) -> str | None:
    run = data["run"]
    if context.metadata["plan_sha256"] != context.plan["plan_sha256"]:
        return "batch_plan_identity_mismatch"
    if run["plan_sha256"] != context.plan["plan_sha256"] or run["trial_id"] not in {
        trial["trial_id"] for trial in context.plan["trials"]
    }:
        return "batch_run_identity_mismatch"
    if "implementation_identity" in context.metadata:
        expected_identity = (
            context.metadata["implementation_identity"]
            if context.allow_tool_change
            else context.implementation_identity
        )
        if not execution_matches(run.get("implementation_identity"), expected_identity):
            return "batch_run_tool_changed"
    elif run.get("tool_source_sha256") != context.expected_tool:
        return "batch_run_tool_changed"
    if run["diagnostic"] != context.metadata["diagnostic"]:
        return "batch_diagnostic_mode_changed"
    return None


def _binding(
    path: Path, data: JSONRecord, trial_metadata: JSONRecord, context: HistoryContext
) -> tuple[JSONRecord | None, bool, str | None]:
    evidence = (
        trial_metadata["evidence"] if context.full_verification else TrialEvidence(path, projection=True)
    )
    binding = evidence.observation("service_binding", stage="prepare")
    sealed = False
    if binding is not None:
        if binding.get("effective_endpoint") != data["config"]["endpoint"]:
            return binding, sealed, "service_binding_differs_from_execution"
        ref = evidence.observation_reference("service_binding", stage="prepare")
        if evidence.manifest is not None:
            if ref["path"] not in evidence.manifest["files"]:
                return binding, sealed, "service_binding_not_sealed"
            verify_reference(path, ref, reads=evidence if context.full_verification else None)
            sealed = True
    elif data["summary"]["counts"]["executed"] or evidence.manifest is not None:
        return binding, sealed, "service_binding_missing"
    return binding, sealed, None


def _frozen_inputs_code(data: JSONRecord, context: HistoryContext) -> str | None:
    trial = next(t for t in context.plan["trials"] if t["trial_id"] == data["run"]["trial_id"])
    base = context.loaded[trial["workload_id"]]
    if (
        immutable_config(data["config"]) != immutable_config(base.config.to_dict())
        or data["bundle"] != base.bundle.to_dict()
    ):
        return "batch_frozen_inputs_changed"
    return None


def _read_run(path: Path, context: HistoryContext) -> RunRead:
    if not path.is_dir() or path.is_symlink():
        return _rejected(path, "unexpected_batch_entry")
    data, trial_metadata = _load_run(path, context)
    run = data["run"]
    code = _identity_code(data, context)
    if code is not None:
        classification, detail = _identity_classification(run, context.plan)
        return _rejected(
            path,
            code,
            run,
            classification=classification or code,
            detail=detail or code,
        )
    binding, sealed, code = _binding(path, data, trial_metadata, context)
    if code is not None:
        return _rejected(path, code, run)
    code = _frozen_inputs_code(data, context)
    if code is not None:
        return _rejected(path, code, run)
    data["path"] = path
    if context.retain_binding:
        data["service_binding"] = binding if sealed else None
        data["service_binding_sealed"] = sealed
    return RunRead(path, data=data)


def _resume_parent_code(
    tid: str, current: JSONRecord, by_id: dict[str, JSONRecord]
) -> str | None:
    run = current["run"]
    if run["parent_run_id"] is None:
        return None
    parent = by_id.get(run["parent_run_id"])
    if parent is None or current["selection"]["parent_events_sha256"] != parent["events_sha256"]:
        return "parent_events_changed_or_missing"
    if run["relation"] != "resume":
        return None
    expected = [
        r["case_id"] for r in parent["requests"] if r["execution_state"] == "not_executed"
    ]
    if parent["run"]["trial_id"] != tid or current["selection"]["case_ids"] != expected:
        return "resume_replaces_attempted_cases"
    return None


def _group_step(
    tid: str,
    current: JSONRecord,
    group: list[JSONRecord],
    by_id: dict[str, JSONRecord],
    visited: set[str],
) -> tuple[JSONRecord | None, str | None]:
    run = current["run"]
    if run["run_id"] in visited:
        return None, "cyclic_resume_history"
    visited.add(run["run_id"])
    code = _resume_parent_code(tid, current, by_id)
    if code is not None:
        return None, code
    children = [d for d in group if d["run"]["parent_run_id"] == run["run_id"]]
    if len(children) > 1:
        return None, "branched_resume_history"
    return (children[0] if children else None), None


def _ordered_group(
    tid: str, group: list[JSONRecord], by_id: dict[str, JSONRecord]
) -> tuple[list[JSONRecord], str | None]:
    roots = [d for d in group if d["run"]["relation"] != "resume"]
    if len(roots) != 1:
        return [], "trial_history_has_multiple_or_missing_roots"
    ordered, current, visited = [], roots[0], set()
    while current is not None:
        following, code = _group_step(tid, current, group, by_id, visited)
        if code is not None:
            return [], code
        ordered.append(current)
        current = following
    if len(visited) != len(group):
        return [], "disconnected_resume_history"
    return ordered, None


@dataclass(frozen=True, slots=True)
class _RunIdentity:
    path: Path
    run_id: str | None
    trial_id: str | None


@dataclass(slots=True)
class _CollectState:
    by_id: dict[str, JSONRecord]
    groups: dict[str, list[JSONRecord]]
    on_error: Callable[[RunRead], None]
    reported_duplicates: set[str]
    seen: dict[str, _RunIdentity]


def _reject_duplicate_run(
    state: _CollectState, original: _RunIdentity, current: _RunIdentity
) -> None:
    if current.run_id not in state.reported_duplicates:
        state.reported_duplicates.add(current.run_id)
        state.on_error(
            _rejected(
                original.path,
                "batch_run_identity_mismatch",
                {"run_id": original.run_id, "trial_id": original.trial_id},
                classification=DUPLICATE_ATTEMPT,
                detail="duplicate_run_id",
            )
        )
    state.on_error(
        _rejected(
            current.path,
            "batch_run_identity_mismatch",
            {"run_id": current.run_id, "trial_id": current.trial_id},
            classification=DUPLICATE_ATTEMPT,
            detail="duplicate_run_id",
        )
    )


def _register_identity(state: _CollectState, identity: _RunIdentity) -> None:
    if identity.run_id is None:
        return
    if identity.run_id in state.seen:
        _reject_duplicate_run(state, state.seen[identity.run_id], identity)
        return
    state.seen[identity.run_id] = identity


def _collect_run(path: Path, context: HistoryContext, state: _CollectState) -> None:
    try:
        outcome = _read_run(path, context)
    except (EvidenceError, ContractError, UnsupportedFormat, OSError, ValueError, KeyError, TypeError, IndexError) as exc:
        identity = _failure_identity(path, context.plan)
        if identity is None:
            state.on_error(RunRead(path, error=exc))
        else:
            rejected = RunRead(
                path,
                code=identity.code,
                error=exc,
                trial_id=identity.trial_id,
                run_id=identity.run_id,
                classification=identity.classification,
                detail=(
                    str(exc) or identity.detail
                    if identity.classification == HISTORY_IDENTITY_ERROR
                    else identity.detail
                ),
            )
            _register_identity(
                state,
                _RunIdentity(path, identity.run_id, identity.trial_id),
            )
            state.on_error(rejected)
        return
    if outcome.code is not None or outcome.error is not None:
        _register_identity(
            state,
            _RunIdentity(outcome.path, outcome.run_id, outcome.trial_id),
        )
        state.on_error(outcome)
        return
    data = outcome.data
    if data is None:
        state.on_error(RunRead(path, error=EvidenceError("invalid_run_projection")))
        return
    run = data["run"]
    duplicate = run["run_id"] in state.seen
    _register_identity(state, _RunIdentity(path, run["run_id"], run["trial_id"]))
    if duplicate:
        return
    state.by_id[run["run_id"]] = data
    state.groups[run["trial_id"]].append(data)


def _ordered_groups(
    groups: dict[str, list[JSONRecord]],
    by_id: dict[str, JSONRecord],
    on_error: Callable[[RunRead], None],
) -> list[JSONRecord]:
    ordered = []
    for tid, group in groups.items():
        if not group:
            continue
        group_ordered, code = _ordered_group(tid, group, by_id)
        if code is not None:
            first = group[0]
            classification = (
                DUPLICATE_ATTEMPT
                if code == "trial_history_has_multiple_or_missing_roots"
                else HISTORY_IDENTITY_ERROR
            )
            on_error(
                RunRead(
                    first["path"],
                    code=code,
                    trial_id=tid,
                    run_id=first["run"]["run_id"],
                    classification=classification,
                    detail=code,
                )
            )
            continue
        ordered.extend(group_ordered)
    return ordered


def _collect_history(
    context: HistoryContext, on_error: Callable[[RunRead], None]
) -> list[JSONRecord]:
    groups = {t["trial_id"]: [] for t in context.plan["trials"]}
    state = _CollectState({}, groups, on_error, set(), {})
    for path in sorted((context.root / "runs").iterdir()):
        _collect_run(path, context, state)
    return _ordered_groups(state.groups, state.by_id, state.on_error)


def _raise_history_error(outcome: RunRead) -> None:
    if outcome.error is not None:
        raise outcome.error
    raise EvidenceError(outcome.code)


def history(
    root: Path,
    plan: JSONRecord,
    loaded: dict[str, LoadedConfig],
    *,
    allow_tool_change: bool = False,
    source_identity: str | None = None,
    implementation_identity: JSONRecord | None = None,
    full_verification: bool = False,
) -> list[JSONRecord]:
    metadata, expected_tool = _history_scope(root, allow_tool_change, source_identity)
    context = HistoryContext(
        root,
        plan,
        loaded,
        metadata,
        expected_tool,
        allow_tool_change,
        implementation_identity,
        full_verification,
        False,
    )
    return _collect_history(context, _raise_history_error)


def history_diagnostics(
    root: Path,
    plan: JSONRecord,
    loaded: dict[str, LoadedConfig],
    options: HistoryOptions | None = None,
) -> tuple[list[JSONRecord], list[JSONRecord]]:
    """Collect per-run rejection reasons for comparison readers (fail-fast stays in history)."""
    options = options or HistoryOptions()
    metadata, expected_tool = _history_scope(root, False, options.source_identity)
    implementation_identity = options.implementation_identity
    if implementation_identity is None:
        from inferyard.implementation_identity import IdentityContext

        implementation_identity = IdentityContext(source_hash=tool_source_hash).value
    context = HistoryContext(
        root,
        plan,
        loaded,
        metadata,
        expected_tool,
        False,
        implementation_identity,
        options.full_verification,
        True,
    )
    diagnostics: list[JSONRecord] = []

    def collect(outcome: RunRead) -> None:
        exc = outcome.error
        reason = outcome.classification or outcome.code
        if reason not in REJECTION_CLASSIFICATIONS:
            reason = HISTORY_IDENTITY_ERROR
        diagnostics.append(
            {
                "reason": reason,
                "path": str(outcome.path),
                "trial_id": outcome.trial_id,
                "run_id": outcome.run_id,
                "error": None
                if exc is None
                else {"type": type(exc).__name__, "reason": str(exc)},
                "detail": outcome.detail or outcome.code,
            }
        )

    runs = _collect_history(context, collect)
    return runs, diagnostics
