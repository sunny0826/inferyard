"""Exact observed-workload applicability; never authorizes performance comparisons."""

from inferyard.evidence.journal import trial_for
from inferyard.evidence.storage import local_file, read_json, sha256_file


def workload_identity(data):
    plan = data["plan"]
    trial = trial_for(plan, data["run"]["trial_id"])
    workload = next(
        row for row in plan["experiment"]["workloads"] if row["workload_id"] == trial["workload_id"]
    )
    result = {
        "workload": workload,
        "definition_versions": plan["experiment"]["definition_versions"],
        "performance_environment": plan["experiment"].get("performance_environment"),
        "capacity_stop": plan["experiment"].get("capacity_stop"),
        "safety": plan["experiment"].get("safety"),
    }
    if "resource_comparison" in plan["experiment"]:
        result["resource_comparison"] = plan["experiment"]["resource_comparison"]
    return result


def bind_target(root, data, protocol, arm_identity, arm_requests, result, collector):
    """Caller supplies independently replayed target and ABBA evidence."""
    from inferyard.runtime.overhead_runner import request_projection

    reasons = []
    if not result["passed"]:
        reasons.append("overhead_preflight_not_passed")
    for kind, filename in (("config", "config.frozen.json"), ("bundle", "bundle.json")):
        if sha256_file(root / filename) != protocol[kind + "_sha256"]:
            reasons.append("target_" + kind + "_mismatch")
    if data["run"].get("tool_source_sha256") != protocol["tool_source_sha256"]:
        reasons.append("target_tool_mismatch")
    if workload_identity(data) != arm_identity:
        reasons.append("target_workload_mismatch")
    rows = [request_projection(row) for row in data["requests"]]
    if [r["case_id"] for r in rows] != protocol["case_ids"]:
        reasons.append("target_case_order_mismatch")
    if data["summary"]["stop_reason"] != "plan_finished" or any(
        r["execution_state"] != "completed" for r in rows
    ):
        reasons.append("target_execution_incomplete")
    if [r["output_identity"] for r in rows] != [r["output_identity"] for r in arm_requests]:
        reasons.append("target_output_work_differs")
    if collector.get("collector") != protocol.get("resource_collector", "linux-resource.v2"):
        reasons.append("target_collector_mismatch")
    if protocol.get("kind") == "collector_overhead_abba.v2":
        sealed = read_json(local_file(root, "manifest.json"))["files"]
        if "observer.json" not in sealed or (
            read_json(local_file(root, "observer.json")) != protocol["observer"]
        ):
            reasons.append("target_common_observer_mismatch")
    if protocol.get("kind") == "collector_overhead_abba.v3":
        sealed = read_json(local_file(root, "manifest.json"))["files"]
        if "boundary-observer.json" not in sealed or (
            read_json(local_file(root, "boundary-observer.json")) != protocol["boundary_observer"]
        ):
            reasons.append("target_boundary_observer_mismatch")
    return {
        "definition": "exact_observed_workload_overhead_binding.v1",
        "applicable": not reasons,
        "reasons": sorted(reasons),
        "target_run_id": data["run"]["run_id"],
        "target_manifest_sha256": sha256_file(root / "manifest.json"),
        "protocol_sha256": protocol["protocol_sha256"],
        "performance_comparison_eligible": False,
        "limitations": [
            "exact_config_includes_service_process_identity",
            "no_extrapolation_to_different_workloads_or_duration_protocols",
            "environment_and_pair_comparability_require_separate_qualification",
        ],
    }
