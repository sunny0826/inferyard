"""Exact observed-workload applicability; never authorizes performance comparisons."""

from inferyard.evidence.journal import trial_for
from inferyard.evidence.storage import EvidenceError, sha256_file
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference


def sealed_observation(evidence, kind, *, stage="prepare"):
    value = evidence.observation(kind, stage=stage)
    ref = evidence.observation_reference(kind, stage=stage) if value is not None else None
    if ref is None or evidence.manifest is None or ref["path"] not in evidence.manifest["files"]:
        raise EvidenceError("overhead_" + kind + "_unsealed")
    verify_reference(evidence.root, ref)
    return value


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

    evidence = TrialEvidence(root)
    reasons = []
    if not result["passed"]:
        reasons.append("overhead_preflight_not_passed")
    for kind in ("config", "bundle"):
        verify_reference(root, evidence.input_reference(kind))
        if evidence.input_digest(kind) != protocol[kind + "_sha256"]:
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
    for protocol_kind, kind, reason in (
        ("collector_overhead_abba.v2", "observer", "target_common_observer_mismatch"),
        ("collector_overhead_abba.v3", "boundary_observer", "target_boundary_observer_mismatch"),
    ):
        if protocol.get("kind") == protocol_kind:
            if not evidence.has_observation(kind, stage="prepare"):
                reasons.append(reason)
            elif sealed_observation(evidence, kind) != protocol[kind]:
                reasons.append(reason)
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
