"""Batch snapshots and history are validated under the host measurement lock."""

import math
from copy import deepcopy
from dataclasses import replace
from urllib.parse import urlsplit

from inferyard import SCHEMA_VERSION
from inferyard.config.loader import validate_runtime_config
from inferyard.config.plan_inputs import read_frozen_plan, source_bytes
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes, read_json
from inferyard.implementation_identity import execution_matches
from inferyard.platforms.identity import PreflightError, process_start_ticks
from inferyard.provenance import tool_source_hash


def open_batch(root, source, diagnostic, *, source_identity=None, implementation_identity=None):
    source_identity = source_identity or tool_source_hash()
    if (root / "batch.json").exists():
        meta = read_json(root / "batch.json")
        plan, loaded = read_frozen_plan(root / "frozen-plan/plan.json")
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


def bind_service(loaded, endpoint_url=None, server_pid=None, api_key_env=None):
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
        args = config["engine"]["startup_args"]
        for i, value in enumerate(list(args)):
            if value in replacements and i + 1 < len(args):
                args[i + 1] = replacements[value]
            elif "=" in value and value.split("=", 1)[0] in replacements:
                option = value.split("=", 1)[0]
                args[i] = option + "=" + replacements[option]
    if api_key_env is not None:
        config["endpoint"]["api_key_env"] = api_key_env
    validate_runtime_config(config)
    binding = {
        "schema_version": SCHEMA_VERSION,
        "source": "operator_binding" if endpoint_url else "frozen_configuration",
        "previous_endpoint": original,
        "effective_endpoint": config["endpoint"],
    }
    return replace(loaded, config=Document.parse("config", config)), binding


def require_replaced_service(previous, config, *, serial_continuation=False):
    from inferyard.runtime.service_reuse import require_transition

    return require_transition(
        previous,
        config,
        process_start_ticks,
        reason="handoff_requires_new_service_process",
        serial_continuation=serial_continuation,
    )


def history(
    root,
    plan,
    loaded,
    *,
    allow_tool_change=False,
    source_identity=None,
    implementation_identity=None,
):
    metadata = read_json(root / "batch.json")
    expected_tool = (
        metadata["tool_source_sha256"]
        if allow_tool_change
        else source_identity or tool_source_hash()
    )
    groups = {t["trial_id"]: [] for t in plan["trials"]}
    by_id = {}
    for path in sorted((root / "runs").iterdir()):
        if not path.is_dir() or path.is_symlink():
            raise EvidenceError("unexpected_batch_entry")
        trial_metadata = {}
        data = read_trial(path, metadata=trial_metadata)
        data["manifest_sealed"] = trial_metadata["manifest"] is not None
        data["service_drain"] = trial_metadata.get("service_drain")
        run = data["run"]
        if (
            run["plan_sha256"] != plan["plan_sha256"]
            or run["trial_id"] not in groups
            or run["run_id"] in by_id
        ):
            raise EvidenceError("batch_run_identity_mismatch")
        if "implementation_identity" in metadata:
            expected_identity = (
                metadata["implementation_identity"]
                if allow_tool_change
                else implementation_identity
            )
            if not execution_matches(run.get("implementation_identity"), expected_identity):
                raise EvidenceError("batch_run_tool_changed")
        elif run["tool_source_sha256"] != expected_tool:
            raise EvidenceError("batch_run_tool_changed")
        if run["diagnostic"] != metadata["diagnostic"]:
            raise EvidenceError("batch_diagnostic_mode_changed")
        binding_path = path / "service-binding.json"
        if binding_path.exists():
            binding = read_json(binding_path)
            if binding.get("effective_endpoint") != data["config"]["endpoint"]:
                raise EvidenceError("service_binding_differs_from_execution")
            if (path / "manifest.json").exists() and "service-binding.json" not in read_json(
                path / "manifest.json"
            )["files"]:
                raise EvidenceError("service_binding_not_sealed")
        elif data["summary"]["counts"]["executed"] or (path / "manifest.json").exists():
            raise EvidenceError("service_binding_missing")
        trial = next(t for t in plan["trials"] if t["trial_id"] == run["trial_id"])
        base = loaded[trial["workload_id"]]
        if (
            immutable_config(data["config"]) != immutable_config(base.config.to_dict())
            or data["bundle"] != base.bundle.to_dict()
        ):
            raise EvidenceError("batch_frozen_inputs_changed")
        data["path"] = path
        by_id[run["run_id"]] = data
        groups[run["trial_id"]].append(data)
    ordered = []
    for tid, group in groups.items():
        if not group:
            continue
        roots = [d for d in group if d["run"]["relation"] != "resume"]
        if len(roots) != 1:
            raise EvidenceError("trial_history_has_multiple_or_missing_roots")
        current, visited = roots[0], set()
        while current is not None:
            run = current["run"]
            if run["run_id"] in visited:
                raise EvidenceError("cyclic_resume_history")
            visited.add(run["run_id"])
            if run["parent_run_id"] is not None:
                parent = by_id.get(run["parent_run_id"])
                if (
                    parent is None
                    or current["selection"]["parent_events_sha256"] != parent["events_sha256"]
                ):
                    raise EvidenceError("parent_events_changed_or_missing")
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
                        raise EvidenceError("resume_replaces_attempted_cases")
            ordered.append(current)
            children = [d for d in group if d["run"]["parent_run_id"] == run["run_id"]]
            if len(children) > 1:
                raise EvidenceError("branched_resume_history")
            current = children[0] if children else None
        if len(visited) != len(group):
            raise EvidenceError("disconnected_resume_history")
    return ordered


def remaining_budget(plan, runs):
    spent = 0
    for data in runs:
        path = data["path"] / "execution-budget.json"
        trial = next(t for t in plan["trials"] if t["trial_id"] == data["run"]["trial_id"])
        if not path.exists() or not (data["path"] / "manifest.json").exists():
            # Crash elapsed time is unknown: reserve its whole declared allocation.
            spent += trial["total_budget_seconds"]
            continue
        value = read_json(path)
        seconds = value.get("elapsed_seconds")
        if (
            value.get("run_id") != data["run"]["run_id"]
            or type(seconds) not in (int, float)
            or not math.isfinite(seconds)
            or seconds < 0
        ):
            raise EvidenceError("invalid_execution_budget_evidence")
        manifest = read_json(data["path"] / "manifest.json")
        if "execution-budget.json" not in manifest["files"]:
            raise EvidenceError("budget_evidence_not_sealed")
        spent += seconds
    return max(0, plan["experiment"]["budget"]["max_wall_seconds"] - spent)
