"""Offline experiment expansion: no inference, environment mutation or credentials."""

from __future__ import annotations

import hashlib
import random
import tomllib
from copy import deepcopy
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.analysis.position import register_position_families
from inferyard.config.loader import load_config, read_document, validate_runtime_config
from inferyard.config.plan_inputs import (
    runtime_snapshot,
    source_bytes,
    validate_workload_inputs,
)
from inferyard.config.plan_math import plan_hash, workload_budget
from inferyard.contracts.validation import ContractError, strict_json_loads, validate_document
from inferyard.contracts.validation_cache import command_validation
from inferyard.evidence.formats import require_core, require_input
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes


def compile_plan(experiment):
    """Expand a validated definition. Source/capability verification is separate."""
    require_input(experiment, "experiment")
    validate_document("experiment", experiment)
    if experiment.get("capacity_stop") is not None and any(
        w["protocol"]["kind"] != "fixed"
        or w["purpose"] != "performance"
        or w["input_target_tokens"] is None
        for w in experiment["workloads"]
    ):
        raise ContractError(
            "experiment.capacity_stop", "requires fixed performance length workloads"
        )
    trials = []
    entries = 0
    for workload in experiment["workloads"]:
        explicit = workload.get("repeat_case_orders")
        if explicit is not None:
            cases = workload["protocol"]["case_ids"]
            if len(explicit) != workload["repeats"] or any(
                len(order) != len(cases) or sorted(order) != sorted(cases) for order in explicit
            ):
                raise ContractError(
                    "experiment.workloads.repeat_case_orders",
                    "one exact permutation per repeat required",
                )
        entries += len(workload["protocol"]["case_ids"]) * workload["repeats"]
        if entries > 100_000:
            raise ContractError("experiment.workloads", "plan case expansion limit exceeded")
        limit, seconds, total = workload_budget(workload)
        # Bound allocation before expansion, even when repeats is maliciously huge.
        if (
            sum(t["request_limit"] for t in trials) + limit * workload["repeats"]
            > experiment["budget"]["max_requests"]
        ):
            raise ContractError("experiment.budget.max_requests", "request budget exceeded")
        if len(trials) + workload["repeats"] > 10_000:
            raise ContractError("experiment.workloads.repeats", "plan expansion limit exceeded")
        for repetition in range(workload["repeats"]):
            key = f"{experiment['experiment_id']}:{workload['workload_id']}:{repetition}"
            trial_id = "trial-" + hashlib.sha256(key.encode()).hexdigest()[:32]
            order = list(workload["protocol"]["case_ids"])
            if explicit is not None:
                order = list(explicit[repetition])
            elif experiment["execution"]["order"] == "seeded":
                seed = hashlib.sha256(f"{experiment['execution']['seed']}:{key}".encode()).digest()
                random.Random(seed).shuffle(order)
            trials.append(
                {
                    "trial_id": trial_id,
                    "workload_id": workload["workload_id"],
                    "repeat_index": repetition,
                    "case_order": order,
                    "request_limit": limit,
                    "request_budget_seconds": seconds,
                    "total_budget_seconds": total,
                    "requires_service_handoff": True,
                }
            )
    plan = {
        "schema_version": SCHEMA_VERSION,
        "plan_sha256": "0" * 64,
        "experiment": deepcopy(experiment),
        "runtime_bindings": [],
        "trials": trials,
        "request_limit": sum(t["request_limit"] for t in trials),
        "request_budget_seconds": sum(t["request_budget_seconds"] for t in trials),
        "total_budget_seconds": sum(t["total_budget_seconds"] for t in trials),
        "limitations": [
            "live_identity_and_capability_checks_required",
            "template_token_budget_requires_live_preflight",
            "operator_service_preparation_time_excluded",
            "overhead_is_a_declared_budget_not_a_measured_prediction",
        ],
    }
    plan["plan_sha256"] = plan_hash(plan)
    require_core(plan, "plan")
    require_core(plan["experiment"], "experiment")
    validate_document("plan", plan)
    return plan


def prepare_plan(source: Path):
    """Read bounded, hash-bound sources; return plan plus exact source bytes."""
    raw = read_document(source)
    try:
        incoming = (
            tomllib.loads(raw.decode())
            if source.suffix == ".toml"
            else strict_json_loads(raw.decode())
        )
    except (UnicodeError, tomllib.TOMLDecodeError) as exc:
        raise ContractError("experiment", "invalid input encoding or TOML") from exc
    plan = compile_plan(incoming)
    artifacts = {}
    position_families = {}
    for workload in incoming["workloads"]:
        loaded = {}
        for kind in ("config", "bundle"):
            reference = workload[kind]
            path, content = source_bytes(source.parent, reference)
            if (
                reference["path"] in ("plan.json", "experiment.json")
                or Path(reference["path"]).parts[0] == "runtime-configs"
            ):
                raise ContractError("experiment.workloads", "reserved artifact name")
            if path.suffix == ".toml" and kind == "config":
                config = load_config(path)
                loaded[kind] = config.config.to_dict()
            else:
                try:
                    loaded[kind] = strict_json_loads(content.decode())
                except UnicodeError as exc:
                    raise ContractError("experiment.workloads", "invalid source encoding") from exc
                require_input(loaded[kind], kind)
                validate_document(kind, loaded[kind])
            previous = artifacts.get(reference["path"])
            if previous is not None and previous != content:
                raise EvidenceError("plan_reference_collision")
            artifacts[reference["path"]] = content
        config, bundle = loaded["config"], loaded["bundle"]
        validate_runtime_config(config)
        validate_workload_inputs(workload, config, bundle)
        register_position_families(workload, bundle, position_families)
        config_path = source.parent / workload["config"]["path"]
        corpus_path = (config_path.parent / config["bundle"]["path"]).resolve()
        if hashlib.sha256(read_document(corpus_path)).hexdigest() != workload["bundle"]["sha256"]:
            raise ContractError("experiment.workloads.bundle", "config and bundle content differ")
        binding, snapshot = runtime_snapshot(
            workload, artifacts[workload["config"]["path"]], config_path.parent.resolve()
        )
        plan["runtime_bindings"].append(binding)
        artifacts[binding["config"]["path"]] = snapshot
    plan["plan_sha256"] = plan_hash(plan)
    require_core(plan, "plan")
    require_core(plan["experiment"], "experiment")
    validate_document("plan", plan)
    return plan, artifacts


def write_plan(source: Path, destination: Path):
    plan, artifacts = prepare_plan(source)
    with command_validation(enabled=False):
        validate_document("plan", plan)
    if destination.resolve() == source.parent.resolve():
        raise EvidenceError("plan_output_overlaps_inputs")
    destination.mkdir(parents=True, exist_ok=False)
    for name, content in artifacts.items():
        path = destination / name
        path.parent.mkdir(parents=True, exist_ok=True)
        atomic_bytes(path, content)
    atomic_bytes(destination / "experiment.json", json_bytes(plan["experiment"]))
    # Publishing the plan last is the completion marker; incomplete output is not executable.
    atomic_bytes(destination / "plan.json", json_bytes(plan))
    return plan
