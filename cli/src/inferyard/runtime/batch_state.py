"""Batch snapshots and history are validated under the host measurement lock."""

import math
from copy import deepcopy
from dataclasses import replace
from collections.abc import Callable
from pathlib import Path
from urllib.parse import urlsplit

from inferyard import SCHEMA_VERSION
from inferyard.config.loader import validate_runtime_config
from inferyard.config.plan_inputs import read_frozen_plan, source_bytes
from inferyard.config.startup_arguments import replace_arguments
from inferyard.contracts.engine_profiles import is_local
from inferyard.contracts.validation import ContractError, Document
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.ledger import read_run_projection, read_trial
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes, read_json
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference
from inferyard.implementation_identity import execution_matches
from inferyard.platforms.identity import PreflightError, hash_file, process_start_ticks
from inferyard.provenance import tool_source_hash


def open_batch(root, source, diagnostic, *, source_identity=None, implementation_identity=None):
    if implementation_identity is None:
        from inferyard.implementation_identity import IdentityContext

        context = IdentityContext(source_hash=tool_source_hash)
        implementation_identity = context.value
        source_identity = source_identity or context.source
    source_identity = source_identity or tool_source_hash()
    if (root / "batch.json").exists():
        meta = read_json(root / "batch.json")
        plan, loaded = read_frozen_plan(root / "frozen-plan/plan.json")
        from inferyard.runtime.context_speed import is_speed

        if is_speed(plan, loaded):
            raise PreflightError("context_speed_requires_new_plan_and_batch")
        if (
            meta.get("schema_version") != SCHEMA_VERSION
            or meta.get("plan_sha256") != plan["plan_sha256"]
            or meta.get("diagnostic") != diagnostic
            or (
                not execution_matches(meta["implementation_identity"], implementation_identity)
                if "implementation_identity" in meta
                else meta.get("tool_source_sha256") != source_identity
            )
        ):
            raise EvidenceError("batch_identity_or_tool_changed")
        if source and read_frozen_plan(source)[0]["plan_sha256"] != plan["plan_sha256"]:
            raise EvidenceError("batch_plan_mismatch")
        return plan, loaded
    if source is None:
        raise EvidenceError("batch_source_plan_required")
    plan, _ = read_frozen_plan(source)
    if root.exists() and any(root.iterdir()):
        raise EvidenceError("new_batch_requires_empty_output_directory")
    root.mkdir(parents=True, exist_ok=True)
    frozen = root / "frozen-plan"
    frozen.mkdir()
    references = [w[k] for w in plan["experiment"]["workloads"] for k in ("config", "bundle")]
    references += [b["config"] for b in plan["runtime_bindings"]]
    artifacts = {}
    for ref in references:
        _, raw = source_bytes(source.parent, ref)
        artifacts[ref["path"]] = raw
    for name, raw in artifacts.items():
        path = frozen / name
        path.parent.mkdir(parents=True, exist_ok=True)
        atomic_bytes(path, raw)
    atomic_bytes(frozen / "plan.json", json_bytes(plan))
    (root / "runs").mkdir()
    atomic_bytes(
        root / "batch.json",
        json_bytes(
            dict(
                schema_version=SCHEMA_VERSION,
                plan_sha256=plan["plan_sha256"],
                diagnostic=diagnostic,
                tool_source_sha256=source_identity,
                **(
                    {"implementation_identity": implementation_identity}
                    if implementation_identity
                    else {}
                ),
            )
        ),
    )
    return read_frozen_plan(frozen / "plan.json")


def _args_without_endpoint(args):
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


def immutable_config(config):
    value = deepcopy(config)
    value.pop("endpoint")
    value["engine"]["startup_args"] = _args_without_endpoint(value["engine"]["startup_args"])
    value["bundle"].pop("path")
    return value


def bind_service(
    loaded, endpoint_url=None, server_pid=None, api_key_env=None, service_binding_path=None
):
    config = loaded.config.to_dict()
    original = deepcopy(config["endpoint"])
    if (endpoint_url is None) != (server_pid is None):
        raise PreflightError("service_binding_requires_endpoint_and_pid")
    if endpoint_url is not None:
        config["endpoint"].update(
            url=endpoint_url,
            server_pid=server_pid,
            process_start_ticks=process_start_ticks(server_pid),
        )
        parsed = urlsplit(endpoint_url)
        replacements = {
            "--host": parsed.hostname,
            "--port": str(parsed.port or (443 if parsed.scheme == "https" else 80)),
            "-p": str(parsed.port or 80),
        }
        config["engine"]["startup_args"] = replace_arguments(
            config["engine"]["startup_args"], replacements
        )
    if api_key_env is not None:
        config["endpoint"]["api_key_env"] = api_key_env
    if service_binding_path is not None:
        apply_local_binding(config, service_binding_path)
    elif is_local(config) and endpoint_url is not None:
        raise PreflightError("local_engine_service_binding_required")
    validate_runtime_config(config)
    binding = {
        "schema_version": SCHEMA_VERSION,
        "source": "operator_binding" if endpoint_url else "frozen_configuration",
        "previous_endpoint": original,
        "effective_endpoint": config["endpoint"],
    }
    return replace(loaded, config=Document.parse("config", config)), binding


def apply_local_binding(config, path):
    from inferyard.platforms.local_engines.assets import bound_json
    from inferyard.platforms.local_engines.service import validate_binding

    if not is_local(config):
        raise PreflightError("local_engine_profile_required")
    identity = hash_file(path)
    binding, _ = bound_json(identity.path, identity.sha256)
    validate_binding(binding, config)
    config["endpoint"].update(binding_path=identity.path, binding_sha256=identity.sha256)


def require_replaced_service(previous, config, *, serial_continuation=False):
    from inferyard.runtime.service_reuse import require_transition

    return require_transition(
        previous,
        config,
        process_start_ticks,
        reason="handoff_requires_new_service_process",
        serial_continuation=serial_continuation,
    )


def _history_scope(
    root: Path, allow_tool_change: bool, source_identity: str | None
) -> tuple[dict, str]:
    metadata = read_json(root / "batch.json")
    expected_tool = (
        metadata.get("tool_source_sha256")
        if allow_tool_change
        else source_identity or tool_source_hash()
    )
    return metadata, expected_tool


def _read_run(
    path: Path,
    plan: dict,
    loaded: dict,
    metadata: dict,
    expected_tool: str,
    *,
    allow_tool_change: bool,
    implementation_identity: dict | None,
    full_verification: bool,
    retain_binding: bool,
) -> tuple[dict | None, str | None]:
    if not path.is_dir() or path.is_symlink():
        return None, "unexpected_batch_entry"
    trial_metadata = {}
    if full_verification:
        data = read_trial(path, metadata=trial_metadata)
        data["manifest_sealed"] = trial_metadata["manifest"] is not None
        data["service_drain"] = trial_metadata.get("service_drain")
        if is_local(data["config"]):
            data["identity"] = trial_metadata["identity"]
    else:
        data = read_run_projection(path)
        data["manifest_sealed"] = data.get("manifest_sealed", False)
    run = data["run"]
    if run["plan_sha256"] != plan["plan_sha256"] or run["trial_id"] not in {
        trial["trial_id"] for trial in plan["trials"]
    }:
        return None, "batch_run_identity_mismatch"
    if "implementation_identity" in metadata:
        expected_identity = (
            metadata["implementation_identity"] if allow_tool_change else implementation_identity
        )
        if not execution_matches(run.get("implementation_identity"), expected_identity):
            return None, "batch_run_tool_changed"
    elif run.get("tool_source_sha256") != expected_tool:
        return None, "batch_run_tool_changed"
    if run["diagnostic"] != metadata["diagnostic"]:
        return None, "batch_diagnostic_mode_changed"
    evidence = (
        trial_metadata["evidence"] if full_verification else TrialEvidence(path, projection=True)
    )
    binding = evidence.observation("service_binding", stage="prepare")
    sealed = False
    if binding is not None:
        if binding.get("effective_endpoint") != data["config"]["endpoint"]:
            return None, "service_binding_differs_from_execution"
        ref = evidence.observation_reference("service_binding", stage="prepare")
        if evidence.manifest is not None:
            if ref["path"] not in evidence.manifest["files"]:
                return None, "service_binding_not_sealed"
            verify_reference(path, ref, reads=evidence if full_verification else None)
            sealed = True
    elif data["summary"]["counts"]["executed"] or evidence.manifest is not None:
        return None, "service_binding_missing"
    trial = next(t for t in plan["trials"] if t["trial_id"] == run["trial_id"])
    base = loaded[trial["workload_id"]]
    if (
        immutable_config(data["config"]) != immutable_config(base.config.to_dict())
        or data["bundle"] != base.bundle.to_dict()
    ):
        return None, "batch_frozen_inputs_changed"
    data["path"] = path
    if retain_binding:
        data["service_binding"] = binding if sealed else None
        data["service_binding_sealed"] = sealed
    return data, None


def _ordered_group(
    tid: str, group: list[dict], by_id: dict[str, dict]
) -> tuple[list[dict], str | None]:
    roots = [d for d in group if d["run"]["relation"] != "resume"]
    if len(roots) != 1:
        return [], "trial_history_has_multiple_or_missing_roots"
    ordered, current, visited = [], roots[0], set()
    while current is not None:
        run = current["run"]
        if run["run_id"] in visited:
            return [], "cyclic_resume_history"
        visited.add(run["run_id"])
        if run["parent_run_id"] is not None:
            parent = by_id.get(run["parent_run_id"])
            if (
                parent is None
                or current["selection"]["parent_events_sha256"] != parent["events_sha256"]
            ):
                return [], "parent_events_changed_or_missing"
            if run["relation"] == "resume":
                expected = [
                    r["case_id"]
                    for r in parent["requests"]
                    if r["execution_state"] == "not_executed"
                ]
                if (
                    parent["run"]["trial_id"] != tid
                    or current["selection"]["case_ids"] != expected
                ):
                    return [], "resume_replaces_attempted_cases"
        ordered.append(current)
        children = [d for d in group if d["run"]["parent_run_id"] == run["run_id"]]
        if len(children) > 1:
            return [], "branched_resume_history"
        current = children[0] if children else None
    if len(visited) != len(group):
        return [], "disconnected_resume_history"
    return ordered, None


def _collect_history(
    root: Path,
    plan: dict,
    loaded: dict,
    metadata: dict,
    expected_tool: str,
    *,
    allow_tool_change: bool,
    implementation_identity: dict | None,
    full_verification: bool,
    retain_binding: bool,
    on_error: Callable[..., None],
) -> list[dict]:
    groups = {t["trial_id"]: [] for t in plan["trials"]}
    by_id = {}
    for path in sorted((root / "runs").iterdir()):
        try:
            data, code = _read_run(
                path,
                plan,
                loaded,
                metadata,
                expected_tool,
                allow_tool_change=allow_tool_change,
                implementation_identity=implementation_identity,
                full_verification=full_verification,
                retain_binding=retain_binding,
            )
        except (EvidenceError, ContractError, UnsupportedFormat, OSError, ValueError, KeyError, TypeError, IndexError) as exc:
            on_error(path=path, code=None, exc=exc)
            continue
        if code is not None:
            on_error(path=path, code=code, exc=None)
            continue
        run = data["run"]
        if run["run_id"] in by_id:
            on_error(path=path, code="batch_run_identity_mismatch", exc=None)
            continue
        by_id[run["run_id"]] = data
        groups[run["trial_id"]].append(data)
    ordered = []
    for tid, group in groups.items():
        if not group:
            continue
        group_ordered, code = _ordered_group(tid, group, by_id)
        if code is not None:
            on_error(path=group[0].get("path"), code=code, exc=None, trial_id=tid)
            continue
        ordered.extend(group_ordered)
    return ordered


def _raise_history_error(
    *, path: Path | None = None, code: str | None, exc: Exception | None, trial_id: str | None = None
) -> None:
    if exc is not None:
        raise exc
    raise EvidenceError(code)


def history(
    root: Path,
    plan: dict,
    loaded: dict,
    *,
    allow_tool_change: bool = False,
    source_identity: str | None = None,
    implementation_identity: dict | None = None,
    full_verification: bool = False,
) -> list[dict]:
    metadata, expected_tool = _history_scope(root, allow_tool_change, source_identity)
    return _collect_history(
        root,
        plan,
        loaded,
        metadata,
        expected_tool,
        allow_tool_change=allow_tool_change,
        implementation_identity=implementation_identity,
        full_verification=full_verification,
        retain_binding=False,
        on_error=_raise_history_error,
    )


def history_diagnostics(
    root: Path,
    plan: dict,
    loaded: dict,
    *,
    source_identity: str | None = None,
    implementation_identity: dict | None = None,
    full_verification: bool = True,
) -> tuple[list[dict], list[dict]]:
    """Collect per-run rejection reasons for comparison readers (fail-fast stays in history)."""
    metadata, expected_tool = _history_scope(root, False, source_identity)
    if implementation_identity is None:
        from inferyard.implementation_identity import IdentityContext

        implementation_identity = IdentityContext(source_hash=tool_source_hash).value
    diagnostics: list[dict] = []

    def collect(
        *,
        path: Path | None = None,
        code: str | None,
        exc: Exception | None,
        trial_id: str | None = None,
    ) -> None:
        reason = code
        if exc is not None:
            reason = "unsupported_format" if isinstance(exc, UnsupportedFormat) else type(exc).__name__
        diagnostics.append(
            {
                "reason": reason,
                "path": None if path is None else str(path),
                "trial_id": trial_id,
                "error": None
                if exc is None
                else {"type": type(exc).__name__, "reason": str(exc)},
                "detail": code,
            }
        )

    runs = _collect_history(
        root,
        plan,
        loaded,
        metadata,
        expected_tool,
        allow_tool_change=False,
        implementation_identity=implementation_identity,
        full_verification=full_verification,
        retain_binding=True,
        on_error=collect,
    )
    return runs, diagnostics


def remaining_budget(plan, runs):
    spent = 0
    for data in runs:
        evidence = TrialEvidence(data["path"], projection=True)
        trial = next(t for t in plan["trials"] if t["trial_id"] == data["run"]["trial_id"])
        if evidence.manifest is None:
            spent += trial["total_budget_seconds"]
            continue
        value = evidence.observation("execution_budget", stage="finalizing")
        if value is None:
            # Crash elapsed time is unknown: reserve its whole declared allocation.
            spent += trial["total_budget_seconds"]
            continue
        seconds = value.get("elapsed_seconds")
        if (
            value.get("run_id") != data["run"]["run_id"]
            or type(seconds) not in (int, float)
            or not math.isfinite(seconds)
            or seconds < 0
        ):
            raise EvidenceError("invalid_execution_budget_evidence")
        ref = evidence.observation_reference("execution_budget", stage="finalizing")
        if ref["path"] not in evidence.manifest["files"]:
            raise EvidenceError("budget_evidence_not_sealed")
        verify_reference(data["path"], ref)
        spent += seconds
    return max(0, plan["experiment"]["budget"]["max_wall_seconds"] - spent)
