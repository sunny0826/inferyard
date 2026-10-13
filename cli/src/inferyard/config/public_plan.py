"""Materialize a public candidate's complete selected workload without generation."""

from copy import deepcopy

from inferyard import SCHEMA_VERSION
from inferyard.analysis.scoring import scorer_hash
from inferyard.application.types import CommandResult
from inferyard.config.planning import compile_plan, write_plan
from inferyard.config.public_config_check import check_config
from inferyard.config.public_recipe import fingerprint
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
    sha256_file,
)
from inferyard.reporting.report_common import _new_output


def materialize(package, loaded, out):
    matched = check_config(package, loaded)
    if not matched["declared_configuration_matches"]:
        raise EvidenceError("public_local_configuration_mismatch")
    candidate = read_json(local_file(package, "candidate.json"))
    if candidate["identities"]["scorer_sha256"] != scorer_hash():
        raise EvidenceError("public_plan_scorer_source_mismatch")
    reproduction = candidate["reproduction"]
    recipe = reproduction["workload"]
    bundle, config = loaded.bundle.to_dict(), loaded.config.to_dict()

    def case_id(ref):
        index = ref["bundle_index"]
        if type(index) is not int or not 0 <= index < len(bundle["cases"]):
            raise EvidenceError("public_case_index_invalid")
        case = bundle["cases"][index]
        if fingerprint(case) != ref["case_sha256"]:
            raise EvidenceError("public_case_hash_mismatch")
        return case["case_id"]

    orders = [[case_id(ref) for ref in order] for order in recipe["repeat_case_orders"]]
    if not orders:
        raise EvidenceError("public_repeat_orders_missing")
    protocol = {"kind": recipe["protocol_kind"], "case_ids": orders[0]}
    if protocol["kind"] == "duration":
        for key in (
            "duration_seconds",
            "max_requests",
            "window_seconds",
            "min_completed_per_case_per_window",
            "drain_timeout_seconds",
        ):
            protocol[key] = recipe[key]
    workload = {
        k: recipe[k]
        for k in (
            "purpose",
            "repeats",
            "timeout_seconds",
            "overhead_budget_seconds",
            "input_target_tokens",
            "output_budget_tokens",
        )
    }
    if "input_tolerance_tokens" in recipe:
        workload["input_tolerance_tokens"] = recipe["input_tolerance_tokens"]
    config["bundle"]["path"] = "bundle.json"
    workload.update(
        workload_id="reproduced-workload",
        protocol=protocol,
        repeat_case_orders=orders,
        config={"path": "config.json", "sha256": fingerprint(config)},
        bundle={"path": "bundle.json", "sha256": fingerprint(bundle)},
    )
    if recipe.get("position_cases"):
        positions = []
        for row in recipe["position_cases"]:
            position = deepcopy(row)
            index = position.pop("bundle_index")
            position["case_id"] = case_id(
                {"bundle_index": index, "case_sha256": fingerprint(bundle["cases"][index])}
            )
            positions.append(position)
        workload["position_cases"] = positions
    experiment = {
        "schema_version": SCHEMA_VERSION,
        "experiment_id": candidate["candidate_id"] + "-reproduction",
        "name": "Public candidate selected workload reproduction",
        "definition_versions": reproduction["definitions"],
        "execution": {
            "concurrency": 1,
            "automatic_retries": 0,
            "order": "fixed",
            "seed": None,
            "service_transition": "operator_verified",
        },
        "comparison": {"mode": "side-by-side", "factor": None},
        "budget": reproduction["budget"],
        "workloads": [workload],
    }
    if "performance_environment" in reproduction:
        experiment["performance_environment"] = reproduction["performance_environment"]
    if "capacity_stop" in reproduction:
        experiment["capacity_stop"] = reproduction["capacity_stop"]
    if reproduction["safety"] is not None:
        experiment["safety"] = reproduction["safety"]
    compile_plan(experiment)
    _new_output(out, [package, loaded.source.parent])
    inputs = out / "input"
    inputs.mkdir()
    for name, value in (("config", config), ("bundle", bundle), ("experiment", experiment)):
        atomic_bytes(inputs / f"{name}.json", json_bytes(value))
    plan = write_plan(inputs / "experiment.json", out / "frozen")
    lineage = {
        "candidate_id": candidate["candidate_id"],
        "public_manifest_sha256": sha256_file(package / "manifest.json"),
        "source_execution": reproduction["plan_execution"],
        "plan_sha256": plan["plan_sha256"],
        "scope": "all repeats of the selected workload; not other workloads from source experiment",
        "generation_requests": 0,
        "limitations": [
            "runtime_preflight_required",
            "artifact_bytes_not_verified",
            "not_independent_operator_acceptance",
        ],
    }
    atomic_bytes(out / "reproduction.json", json_bytes(lineage))
    return lineage


def execute(request):
    result = materialize(request.run, request.config, request.out)
    return 0, CommandResult(
        request.command,
        "planned",
        "incomplete",
        evidence_dir=str(request.out),
        details=result,
        limitations=tuple(result["limitations"]),
    )
