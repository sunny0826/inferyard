"""A formal trial requires source-matched, preceding full-lifecycle observation evidence."""

from pathlib import Path

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, local_file, read_json, sha256_file
from inferyard.extensions.extension_evidence import verify_packet
from inferyard.runtime.overhead_binding import workload_identity
from inferyard.runtime.overhead_runner import request_projection


def load_total_binding(overhead, target, target_clock):
    filename = "total-control-binding.json"
    path = local_file(overhead, filename)
    if not path.is_file():
        return {"eligible": False, "reasons": ["formal_trial_total_observer_evidence_missing"]}
    reference = read_json(path)
    if (
        not isinstance(reference, dict)
        or set(reference) != {"definition", "path", "manifest_sha256"}
        or reference["definition"] != "total_observer_binding.v1"
        or not isinstance(reference["path"], str)
        or not Path(reference["path"]).is_absolute()
    ):
        raise EvidenceError("total_control_binding_reference_invalid")
    root = Path(reference["path"])
    if sha256_file(local_file(root, "manifest.json")) != reference["manifest_sha256"]:
        raise EvidenceError("total_control_binding_manifest_mismatch")
    assessment = verify_packet(root)
    packet = read_json(local_file(root, "packet.json"))
    spec, arms, guard = packet["spec"], packet["rows"], packet.get("guard", {})
    reasons = []
    if spec["definition"] != "total_observer_control.v2":
        reasons.append("formal_trial_total_observer_protocol_required")
    if packet["evidence_kind"] != "live" or assessment.get("hardware_qualified") is not True:
        reasons.append("full_lifecycle_observer_not_qualified")
    data = read_trial(target)
    for key, filename in (("config", "config.frozen.json"), ("bundle", "bundle.json")):
        if spec.get(key + "_sha256") != sha256_file(local_file(target, filename)):
            reasons.append("total_control_target_" + key + "_mismatch")
    if spec["tool_source_sha256"] != data["run"]["tool_source_sha256"]:
        reasons.append("total_control_target_tool_mismatch")
    expected = [request_projection(row) for row in data["requests"]]
    if [r["case_id"] for r in expected] != spec["case_ids"]:
        reasons.append("total_control_target_case_order_mismatch")
    if len(arms) != 4 or any(
        [r["output_sha256"] for r in arm["requests"]] != [r["output_identity"] for r in expected]
        for arm in arms
    ):
        reasons.append("total_control_target_output_work_differs")
    if (
        target_clock["boot_id"] is None
        or guard.get("clock_id") != target_clock["boot_id"] + ":CLOCK_MONOTONIC"
        or target_clock["start_ns"] is None
        or any(arm["after_close_ns"] >= target_clock["start_ns"] for arm in arms)
    ):
        reasons.append("total_control_not_before_target_in_same_boot")
    if spec["definition"] == "total_observer_control.v2":
        for arm in arms:
            if arm["mode"] == "on":
                control = read_trial(local_file(root, arm["trial_path"]))
                if workload_identity(control) != workload_identity(data):
                    reasons.append("total_control_target_workload_mismatch")
                collector = read_json(local_file(root / arm["trial_path"], "collector.json"))
                target_collector = read_json(local_file(target, "collector.json"))
                for key in ("collector", "sensors", "interval_ms"):
                    if collector.get(key) != target_collector.get(key):
                        reasons.append("total_control_target_collector_mismatch")
    return {
        "definition": "formal_trial_total_observer_binding.v1",
        "eligible": not reasons,
        "reasons": sorted(set(reasons)),
        "tolerance_ratio": spec["tolerance_ratio"],
        "assessment": assessment,
        "source": {**reference, "binding_sha256": sha256_file(path)},
        "limitations": [
            "exact_frozen_workload_and_service_only",
            "baseline_response_preservation_is_not_zero_cost",
            "common_independent_guard_and_dirty_lock_cost_not_estimated",
        ],
    }
