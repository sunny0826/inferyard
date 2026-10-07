"""Hash-bound runtime snapshots preserve host paths when a plan package moves."""

import hashlib
import tomllib
from copy import deepcopy
from pathlib import Path

from inferyard.config.loader import (
    PATH_FIELDS,
    LoadedConfig,
    read_document,
    validate_runtime_config,
)
from inferyard.contracts.schemas import CONFIG_DEFAULTS
from inferyard.contracts.validation import (
    ContractError,
    Document,
    strict_json_loads,
    validate_document,
)
from inferyard.evidence.formats import require_core, require_input
from inferyard.evidence.storage import EvidenceError, json_bytes


def normalized_config(raw, filename, source_directory, bundle_path):
    """Resolve external runtime paths at freeze time; corpus stays package-relative."""
    try:
        config = (
            tomllib.loads(raw.decode())
            if filename.endswith(".toml")
            else strict_json_loads(raw.decode())
        )
    except (UnicodeError, tomllib.TOMLDecodeError) as exc:
        raise ContractError("plan.config", "invalid source encoding or TOML") from exc
    defaulted = []
    if filename.endswith(".toml"):
        require_input(config, "config")
        validate_document("config_input", config)
        for section, defaults in CONFIG_DEFAULTS.items():
            target = config.setdefault(section, {})
            for key, value in defaults.items():
                if key not in target:
                    target[key] = deepcopy(value)
                    defaulted.append(f"{section}.{key}")
    validate_runtime_config(config)
    origin = Path(source_directory)
    for section, key in PATH_FIELDS:
        if config["engine"]["adapter"] in ("kvmem", "ninfer"):
            from pathlib import PureWindowsPath

            if PureWindowsPath(config[section][key]).is_absolute():
                continue
        config[section][key] = str((origin / config[section][key]).resolve())
    if "component_ledger_path" in config["model"] and config["engine"]["adapter"] not in (
        "kvmem",
        "ninfer",
    ):
        config["model"]["component_ledger_path"] = str(
            (origin / config["model"]["component_ledger_path"]).resolve()
        )
    for key in ("parameter_evidence", "service_evidence"):
        config["evidence"][key] = [
            str((origin / value).resolve()) for value in config["evidence"][key]
        ]
    config["bundle"]["path"] = bundle_path
    return config, tuple(defaulted)


def runtime_snapshot(workload, raw, source_directory):
    config, _ = normalized_config(
        raw, workload["config"]["path"], source_directory, workload["bundle"]["path"]
    )
    content = json_bytes(config)
    digest = hashlib.sha256(content).hexdigest()
    return dict(
        workload_id=workload["workload_id"],
        source_directory=str(source_directory),
        config=dict(path=f"runtime-configs/{digest}.json", sha256=digest),
    ), content


def source_bytes(root, reference):
    path = root / reference["path"]
    if not path.resolve().is_relative_to(root.resolve()):
        raise EvidenceError("plan_reference_escapes_input_directory")
    raw = read_document(path)
    if hashlib.sha256(raw).hexdigest() != reference["sha256"]:
        raise EvidenceError("plan_source_hash_mismatch")
    return path, raw


def validate_workload_inputs(workload, config, bundle):
    from inferyard.analysis.position import validate_position_cases
    from inferyard.config.model_lineage import validate_lineage
    from inferyard.runtime.cache_execution import validate_inputs as validate_cache
    from inferyard.runtime.fixed_output import validate_inputs

    if config["engine"]["adapter"] in ("kvmem", "ninfer") and (
        workload["purpose"] != "quality" or workload["protocol"]["kind"] != "fixed"
    ):
        raise ContractError("workload", "lab adapters currently support fixed serial quality text")
    validate_inputs(workload, config, bundle)
    validate_cache(workload, config)
    validate_lineage(workload, config)
    validate_position_cases(workload, bundle)
    if config["bundle"]["version"] != bundle["version"]:
        raise ContractError("experiment.workloads.bundle", "config and bundle version differ")
    ids = {c["case_id"] for c in bundle["cases"]}
    if not set(workload["protocol"]["case_ids"]).issubset(ids):
        raise ContractError("experiment.workloads.protocol.case_ids", "case absent from bundle")
    if workload["output_budget_tokens"] != config["generation"]["max_tokens"]:
        raise ContractError("experiment.workloads.output_budget_tokens", "differs from config")
    target = workload["input_target_tokens"] or 0
    if (
        target + workload.get("input_tolerance_tokens", 0) + workload["output_budget_tokens"]
        > config["conditions"]["context_size"]
    ):
        raise ContractError("experiment.workloads", "declared context budget exceeded")
    if workload["timeout_seconds"] != config["execution"]["timeout_seconds"]:
        raise ContractError("experiment.workloads.timeout_seconds", "differs from config")
    if workload["purpose"] == "quality" and bundle.get("task_protocol") == "performance":
        raise ContractError("experiment.workloads.purpose", "performance corpus has no quality")


def read_frozen_plan(path):
    """Verify all frozen inputs without accessing the original source directory."""
    root = path.parent.resolve()
    plan = strict_json_loads(read_document(path).decode())
    require_core(plan, "plan")
    require_core(plan["experiment"], "experiment")
    validate_document("plan", plan)
    if not plan["runtime_bindings"]:
        raise EvidenceError("plan_runtime_bindings_missing")
    from inferyard.config.planning import compile_plan

    if plan["trials"] != compile_plan(plan["experiment"])["trials"]:
        raise EvidenceError("plan_trials_differ_from_frozen_expansion")
    workloads = {w["workload_id"]: w for w in plan["experiment"]["workloads"]}
    loaded = {}
    position_families = {}
    from inferyard.analysis.position import register_position_families

    for binding in plan["runtime_bindings"]:
        workload = workloads[binding["workload_id"]]
        _, raw = source_bytes(root, workload["config"])
        _, bundle_raw = source_bytes(root, workload["bundle"])
        bundle = strict_json_loads(bundle_raw.decode())
        require_core(bundle, "bundle")
        validate_document("bundle", bundle)
        _, snapshot_raw = source_bytes(root, binding["config"])
        snapshot = strict_json_loads(snapshot_raw.decode())
        expected, defaulted = normalized_config(
            raw, workload["config"]["path"], binding["source_directory"], workload["bundle"]["path"]
        )
        if json_bytes(snapshot) != json_bytes(expected):
            # Earlier v3 snapshots retained this one optional source path verbatim.
            # Accept only that exact, hash-bound old projection, then resolve the
            # runtime value against its frozen origin rather than today's cwd.
            legacy = deepcopy(expected)
            source = (
                tomllib.loads(raw.decode())
                if workload["config"]["path"].endswith(".toml")
                else strict_json_loads(raw.decode())
            )
            if "component_ledger_path" in source["model"]:
                legacy["model"]["component_ledger_path"] = source["model"]["component_ledger_path"]
            if json_bytes(snapshot) != json_bytes(legacy):
                raise EvidenceError("plan_runtime_snapshot_differs_from_source")
        validate_workload_inputs(workload, snapshot, bundle)
        register_position_families(workload, bundle, position_families)
        config = deepcopy(expected)
        config["bundle"]["path"] = str(root / workload["bundle"]["path"])
        loaded[workload["workload_id"]] = LoadedConfig(
            root / binding["config"]["path"],
            Document.parse("config", config),
            Document.parse("bundle", bundle),
            binding["config"]["sha256"],
            workload["bundle"]["sha256"],
            defaulted,
        )
    return plan, loaded
