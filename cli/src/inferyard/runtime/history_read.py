"""Single-run history validation: full evidence verification or projection."""

from __future__ import annotations

from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path

from inferyard.analysis.comparison_history import (
    HISTORY_IDENTITY_ERROR,
    RUN_PLAN_MISMATCH,
    UNPLANNED_TRIAL,
)
from inferyard.contracts.comparison_types import JSONRecord
from inferyard.config.loader import LoadedConfig
from inferyard.contracts.engine_profiles import is_local
from inferyard.contracts.validation import validate_document
from inferyard.analysis.ledger import read_run_projection, read_trial
from inferyard.errors import ContractError, EvidenceError, UnsupportedFormat
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference
from inferyard.implementation_identity import execution_matches


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
