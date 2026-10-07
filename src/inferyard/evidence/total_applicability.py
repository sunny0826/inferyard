"""Versioned, narrow reuse of verified total-observer evidence for comparison v3."""

from copy import deepcopy
from pathlib import Path

from inferyard.analysis.environment_identity import fields
from inferyard.evidence.performance_evidence import clock_span
from inferyard.evidence.storage import EvidenceError, local_file, read_json, sha256_file
from inferyard.extensions.extension_evidence import verify_packet
from inferyard.implementation_identity import role_identity
from inferyard.runtime.overhead_binding import workload_identity
from inferyard.runtime.overhead_runner import read_overhead, request_projection


def config_domain(config):
    """Drop explicit locators only; measured content and loading/generation stay bound."""
    value = deepcopy(config)
    paths = []
    for section, keys in (
        ("model", ("local_path", "template_path", "display_name")),
        ("engine", ("binary_path", "runtime_library_manifest")),
        ("bundle", ("path",)),
    ):
        for key in keys:
            old = value[section].pop(key, None)
            if key != "display_name" and isinstance(old, str):
                paths.append(old)
    value.pop("output", None)
    value.pop("endpoint", None)
    binding = value.get("quantization_artifact_binding")
    if binding is not None:
        # Lineage content is in the frozen workload; mounted paths are locators.
        value.pop("quantization_artifact_binding")
    from inferyard.analysis.comparison_helpers import comparable_arguments

    arguments = comparable_arguments(value["engine"]["startup_args"])
    value["engine"]["startup_args"] = [
        "<bound-asset>"
        if arg in paths
        else arg.split("=", 1)[0] + "=<bound-asset>"
        if "=" in arg and arg.split("=", 1)[1] in paths
        else arg
        for arg in arguments
    ]
    return value


def workload_domain(data):
    value = deepcopy(workload_identity(data))
    workload = value["workload"]
    for key in ("config", "bundle"):
        workload.pop(key, None)
    # Comparison policy does not change collection; keep measurement/scoring definitions.
    value["definition_versions"].pop("comparison", None)
    return value


def domain_reasons(control, data, control_metadata):
    reasons = []
    identities = [
        role_identity(d["run"].get("implementation_identity"), "measurement")
        for d in (control, data)
    ]
    if identities[0] is None or identities[0] != identities[1]:
        reasons.append("total_control_measurement_identity_unknown_or_different")
    if config_domain(control["config"]) != config_domain(data["config"]):
        reasons.append("total_control_target_loading_or_request_config_mismatch")
    if control["bundle"] != data["bundle"] or workload_domain(control) != workload_domain(data):
        reasons.append("total_control_target_workload_mismatch")
    if (
        control_metadata.get("identity", {}).get("verification") != "verified"
        or data.get("identity", {}).get("verification") != "verified"
    ):
        reasons.append("total_control_execution_identity_unverified")
    if control_metadata.get("identity", {}).get("runtime_library_hashes") != data.get(
        "identity", {}
    ).get("runtime_library_hashes"):
        reasons.append("total_control_runtime_libraries_differ")
    environments = [
        control_metadata.get("environment_start", {}),
        data.get("environment_start", {}),
    ]
    for field in fields(environments, include_policy=True):
        if environments[0].get(field) is None or environments[0].get(field) != environments[1].get(
            field
        ):
            reasons.append("total_control_environment_unknown_or_different:" + field)
    for item in (control, data):
        if (
            item["summary"]
            .get("measurement_context", {})
            .get("environment_qualification", {})
            .get("eligible")
            is not True
        ):
            reasons.append("total_control_environment_not_qualified")
    return reasons


def load_total_applicability(overhead, target, *, data, source_roots=()):
    path = local_file(overhead, "total-control-binding.json")
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
    if source_roots:
        from inferyard.evidence.source_locations import resolve_source

        root = resolve_source(overhead, reference["path"], source_roots)
    if sha256_file(local_file(root, "manifest.json")) != reference["manifest_sha256"]:
        raise EvidenceError("total_control_binding_manifest_mismatch")
    verified_trials = {}
    assessment = verify_packet(root, verified_trials=verified_trials)
    packet = read_json(local_file(root, "packet.json"))
    spec, arms, guard = packet["spec"], packet["rows"], packet.get("guard", {})
    reasons = []
    if spec["definition"] != "total_observer_control.v2":
        reasons.append("formal_trial_total_observer_protocol_required")
    if packet["evidence_kind"] != "live" or assessment.get("hardware_qualified") is not True:
        reasons.append("full_lifecycle_observer_not_qualified")
    expected = [request_projection(row) for row in data["requests"]]
    if [r["case_id"] for r in expected] != spec["case_ids"]:
        reasons.append("total_control_target_case_order_mismatch")
    if any(r["execution_state"] != "completed" or r["output_identity"] is None for r in expected):
        reasons.append("total_control_target_execution_incomplete")
    if len(arms) != 4 or any(
        [r["output_sha256"] for r in arm["requests"]] != [r["output_identity"] for r in expected]
        for arm in arms
    ):
        reasons.append("total_control_target_output_work_differs")
    clock = clock_span(target)
    if clock["boot_id"] is None or guard.get("clock_id") != clock["boot_id"] + ":CLOCK_MONOTONIC":
        reasons.append("total_control_cross_boot_or_unknown_not_qualified")
    if clock["start_ns"] is None or any(arm["after_close_ns"] >= clock["start_ns"] for arm in arms):
        reasons.append("total_control_not_before_target")
    target_collector = read_json(local_file(target, "collector.json"))
    if len(verified_trials) != 2:
        reasons.append("total_control_verified_on_arms_required")
    for relative, (control, metadata) in verified_trials.items():
        reasons.extend(domain_reasons(control, data, metadata))
        collector = read_json(local_file(root / relative, "collector.json"))
        if any(
            collector.get(k) != target_collector.get(k)
            for k in ("collector", "sensors", "interval_ms")
        ):
            reasons.append("total_control_target_collector_mismatch")
    return {
        "definition": "total-observer-applicability.v2",
        "eligible": not reasons,
        "reasons": sorted(set(reasons)),
        "tolerance_ratio": spec["tolerance_ratio"],
        "assessment": assessment,
        "source": {**reference, "binding_sha256": sha256_file(path)},
        "limitations": [
            "same_boot_preceding_calibration_only",
            "exact_observed_output_work_only",
            "baseline_response_preservation_is_not_zero_cost",
            "common_independent_guard_and_dirty_lock_cost_not_estimated",
        ],
    }


def load_performance_evidence_v3(root, target, *, data, reference, source_roots=()):
    total = load_total_applicability(
        root, target, data=data, **({"source_roots": source_roots} if source_roots else {})
    )
    incremental = {"status": "not_supplied"}
    assessment = {}
    if (root / "protocol.json").exists():
        # Independently validate diagnostic evidence, without re-reading the target or
        # using an incremental estimate to grant total-observer qualification.
        assessment = read_overhead(root, target=target, target_data=data)
        incremental = {
            "status": "passed" if assessment["passed"] else "limited",
            "assessment": assessment,
            "protocol_sha256": sha256_file(local_file(root, "protocol.json")),
            "trials_sha256": sha256_file(local_file(root, "trials.json")),
        }
    qualified_assessment = {}
    if (
        assessment
        and assessment["passed"]
        and assessment["target_binding"]["applicable"]
        and assessment["environment_binding"]["eligible"]
    ):
        clock = clock_span(target)
        arms = [
            clock_span(local_file(root, entry["path"]))
            for entry in read_json(local_file(root, "trials.json"))
        ]
        if (
            clock["boot_id"]
            and clock["start_ns"] is not None
            and len(arms) == 4
            and all(
                arm["boot_id"] == clock["boot_id"]
                and arm["end_ns"] is not None
                and arm["end_ns"] < clock["start_ns"]
                for arm in arms
            )
        ):
            qualified_assessment = assessment
    if sha256_file(local_file(target, "manifest.json")) != reference["manifest_sha256"]:
        raise EvidenceError("performance_target_changed_during_read")
    return {
        "eligible": total["eligible"],
        "reasons": total["reasons"],
        "target_run_id": data["run"]["run_id"],
        "target_manifest_sha256": reference["manifest_sha256"],
        "tolerance_ratio": total.get("tolerance_ratio"),
        "source": {"path": str(root.resolve())},
        "assessment": qualified_assessment,
        "incremental_diagnostic": incremental,
        "total_observer_binding": total,
    }
