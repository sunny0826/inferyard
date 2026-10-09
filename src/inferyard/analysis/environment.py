"""Evidence-backed environment stability and collector schedule diagnostics.

A quiet schedule is not evidence of negligible measurement perturbation. The
separate frozen ABBA workload overhead gate is deliberately retained.
"""

from inferyard.analysis.environment_identity import LINUX_FIELDS, fields
from inferyard.config.environment_binding import requires_match
from inferyard.evidence.storage import (
    EvidenceError,
    local_file,
    read_json,
    read_jsonl,
    sha256_file,
)
from inferyard.platforms.cpu_policy import assess as assess_cpu_policies
from inferyard.platforms.external_cpu import reduce_external_cpu
from inferyard.platforms.external_cpu_windows import assess_windows

FIELDS = LINUX_FIELDS


def assess_environment(start, end, observations, conditions, requests):
    reasons = set()
    if not start or not end:
        reasons.add("environment_endpoints_missing")
    snapshots = [start]
    times = []
    for item in observations:
        if (
            type(item.get("monotonic_ns")) is not int
            or item["monotonic_ns"] < 0
            or type(item.get("snapshot")) is not dict
        ):
            raise EvidenceError("invalid_environment_observation")
        times.append(item["monotonic_ns"])
        snapshots.append(item["snapshot"])
    snapshots.append(end)
    if not times:
        reasons.add("environment_timeline_missing")
    if any(b <= a for a, b in zip(times, times[1:], strict=False)):
        raise EvidenceError("environment_clock_not_increasing")
    for field in fields(snapshots):
        values = [snapshot.get(field) for snapshot in snapshots]
        if any(value is None for value in values):
            reasons.add("environment_unknown:" + field)
        elif any(value != values[0] for value in values[1:]):
            reasons.add("environment_changed:" + field)
        if (
            field in conditions
            and requires_match(conditions, field)
            and any(value != conditions[field] for value in values)
        ):
            reasons.add("frozen_environment_mismatch:" + field)
    for field in ("pswpin", "pswpout"):
        values = [snapshot.get("swap_pages", {}).get(field) for snapshot in snapshots]
        if any(type(value) is not int or value < 0 for value in values):
            reasons.add("swap_counter_unknown:" + field)
        elif any(b < a for a, b in zip(values, values[1:], strict=False)):
            reasons.add("swap_counter_reset:" + field)
        elif values[-1] > values[0]:
            reasons.add("system_swap_activity:" + field)
    # One-second environmental polling cannot prove absence of faster changes.
    # Require observations no farther than two polling periods from each request edge.
    gaps = []
    for row in requests:
        left, right = row.get("t_send_ns"), row.get("t_terminal_ns")
        if left is None or right is None:
            continue
        inside = [t for t in times if left <= t <= right]
        points = [left, *inside, right]
        gap = max((b - a for a, b in zip(points, points[1:], strict=False)), default=0)
        gaps.append(gap)
        if not inside and not any(abs(t - left) <= 1_000_000_000 for t in times):
            reasons.add("environment_request_window_unobserved")
        if gap > 2_000_000_000:
            reasons.add("environment_sampling_gap")
    if any(s.get("platform") == "Darwin" for s in snapshots):
        from inferyard.platforms.power_macos import assess

        policies = assess([s.get("macos_power_policy") for s in snapshots], conditions)
    else:
        policies = assess_cpu_policies([s.get("cpu_policies") for s in snapshots], conditions)
    reasons.update(policies["reasons"])
    return {
        "cpu_policies": policies,
        "stable_observed_environment": not reasons,
        "reasons": sorted(reasons),
        "observation_count": len(times),
        "largest_request_gap_ns": max(gaps, default=None),
        "limitations": [
            "changes_between_observations_unknown",
            "cpu_policy_inventory_missing_or_incomplete"
            if not policies["complete_and_stable"]
            else "cpu_policies_observed_not_atomic",
            "external_load_not_fully_observed",
        ],
        "comparison_eligible": False,
    }


def assess_schedule(records, interval_ms):
    periodic = []
    boundaries = []
    for row in records:
        if row.get("kind") == "resource_boundary":
            left, right = row.get("read_started_ns"), row.get("read_finished_ns")
            if type(left) is not int or type(right) is not int or left < 0 or right < left:
                raise EvidenceError("invalid_resource_boundary_schedule")
            boundaries.append(right - left)
            continue
        keys = ("scheduled_ns", "actual_ns", "late_ns", "collector_work_ns")
        if any(type(row.get(k)) is not int or row[k] < 0 for k in keys):
            raise EvidenceError("invalid_collector_schedule")
        periodic.append(row)
    return {
        "periodic_samples": len(periodic),
        "boundary_samples": len(boundaries),
        "max_work_ns": max((r["collector_work_ns"] for r in periodic), default=None),
        "max_late_ns": max((r["late_ns"] for r in periodic), default=None),
        "work_exceeds_interval_count": sum(
            r["collector_work_ns"] > interval_ms * 1_000_000 for r in periodic
        ),
        "boundary_work_ns": sum(boundaries),
        "overhead_gate": "not_verified",
        "limitations": [
            "schedule_cost_is_not_workload_perturbation",
            "frozen_abba_overhead_experiment_required",
        ],
    }


def environment_qualification(environment, external_requests, schedule, requests, interval_ms):
    """Environmental prerequisite only; workload comparability and overhead are separate."""
    reasons = set(environment["reasons"])
    if not environment["stable_observed_environment"]:
        reasons.add("environment_not_stable_and_complete")
    if not environment["cpu_policies"]["complete_and_stable"]:
        reasons.add("cpu_policy_qualification_failed")
    if not external_requests["all_requests_eligible"]:
        reasons.add("external_load_qualification_failed")
    for row in external_requests["requests"]:
        reasons.update(row["reasons"])
    if not requests or any(r["execution_state"] not in ("completed", "failed") for r in requests):
        reasons.add("request_execution_scope_incomplete")
    if schedule["periodic_samples"] == 0:
        reasons.add("collector_schedule_missing")
    if schedule["work_exceeds_interval_count"]:
        reasons.add("collector_work_exceeds_interval")
    if schedule["max_late_ns"] is None or schedule["max_late_ns"] > interval_ms * 2_000_000:
        reasons.add("collector_schedule_lateness_unknown_or_excessive")
    return {
        "definition": "observed_environment_qualification.v1",
        "eligible": not reasons,
        "reasons": sorted(reasons),
        "request_count": len(requests),
        "limitations": [
            "sampled_conditions_not_proof_of_continuous_stationarity",
            "external_load_is_bracketing_interval_average",
            "environment_prerequisite_not_pair_comparison_authorization",
            "matching_workload_collector_overhead_evidence_still_required",
        ],
    }


def measurement_context(root, manifest_files, config, requests, *, performance_policy=None):
    files = (
        "environment.start.json",
        "environment.end.json",
        "environment.jsonl",
        "schedule.jsonl",
    )
    evidence, missing, data = [], [], {}
    for name in files:
        if name not in manifest_files:
            missing.append("unsealed_or_missing:" + name)
            data[name] = [] if name.endswith("jsonl") else {}
            continue
        path = local_file(root, name)
        if name.endswith("jsonl"):
            data[name], truncated = read_jsonl(path)
            missing.extend(name + ":" + issue for issue in truncated)
        else:
            data[name] = read_json(path)
            if type(data[name]) is not dict:
                raise EvidenceError("invalid_environment_snapshot")
        evidence.append({"path": name, "sha256": sha256_file(path)})
    environment = assess_environment(
        data[files[0]], data[files[1]], data[files[2]], config["conditions"], requests
    )
    environment["reasons"] = sorted(set(environment["reasons"] + missing))
    environment["stable_observed_environment"] &= not missing
    external = []
    if "external-cpu.jsonl" in manifest_files:
        path = local_file(root, "external-cpu.jsonl")
        external, issues = read_jsonl(path)
        if issues:
            raise EvidenceError("invalid_external_cpu_jsonl")
        evidence.append({"path": "external-cpu.jsonl", "sha256": sha256_file(path)})
    external = reduce_external_cpu(external, config["endpoint"], config["telemetry"]["interval_ms"])
    external_requests = assess_windows(external, requests, performance_policy)
    schedule = assess_schedule(data[files[3]], config["telemetry"]["interval_ms"])
    return {
        "environment_qualification": environment_qualification(
            environment, external_requests, schedule, requests, config["telemetry"]["interval_ms"]
        ),
        "external_cpu": external,
        "external_cpu_requests": external_requests,
        "environment": environment,
        "collector_schedule": schedule,
        "evidence_refs": evidence,
        "performance_comparison_eligible": False,
    }
