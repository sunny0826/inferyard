"""Rebuild the bounded, outside-request baseline environment qualification."""

from inferyard.analysis.environment import assess_environment
from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_reads import TrialReads
from inferyard.platforms.external_cpu import reduce_external_cpu
from inferyard.platforms.external_cpu_windows import assess_windows
from inferyard.runtime.boundary_observer import boundary_contract


def qualify_boundary_environment(root, data, sealed, *, reads=None):
    reads = reads if reads is not None else TrialReads(root)
    reasons, evidence = set(), []
    required = {"boundary-observer.json", "request-environment.jsonl"}
    if not required <= set(sealed):
        return {
            "eligible": False,
            "reasons": ["boundary_environment_evidence_missing"],
            "evidence_refs": [],
        }
    observed_contract = reads.checked_json("boundary-observer.json")
    collector = observed_contract.get("collector", "linux-resource.v2")
    if collector not in ("linux-resource.v2", "macos-resource.v1") or (
        observed_contract != boundary_contract(collector=collector)
    ):
        raise EvidenceError("boundary_observer_contract_mismatch")
    records, issues = reads.checked_jsonl("request-environment.jsonl")
    if issues:
        raise EvidenceError("invalid_boundary_environment_log")
    for name in sorted(required):
        evidence.append({"path": name, "sha256": reads.hashes[name]})
    by_request = {}
    last = -1
    for record in records:
        start, end = record.get("read_started_ns"), record.get("read_finished_ns")
        if (
            type(start) is not int
            or type(end) is not int
            or start < 0
            or start < last
            or end < start
            or record.get("boundary") not in ("before_send", "after_terminal")
            or not isinstance(record.get("environment"), dict)
            or not isinstance(record.get("cpu"), dict)
        ):
            raise EvidenceError("invalid_boundary_environment_record")
        last = end
        cpu = record["cpu"]
        if (
            cpu.get("phase") != record.get("phase")
            or cpu.get("request_id") != record.get("request_id")
            or type(cpu.get("read_started_ns")) is not int
            or type(cpu.get("read_finished_ns")) is not int
            or not start <= cpu["read_started_ns"] <= cpu["read_finished_ns"] <= end
        ):
            raise EvidenceError("boundary_cpu_observation_binding_mismatch")
        by_request.setdefault(record.get("request_id"), []).append(record)
    policy = data["plan"]["experiment"].get("performance_environment")
    if policy is None:
        reasons.add("performance_environment_policy_missing")
    observations, windows = [], []
    requests = data["requests"]
    if not requests:
        reasons.add("request_execution_scope_incomplete")
    for request in requests:
        key = request["request_id"]
        pair = by_request.get(key, [])
        left, right = request.get("t_send_ns"), request.get("t_terminal_ns")
        if (
            len(pair) != 2
            or [r["boundary"] for r in pair] != ["before_send", "after_terminal"]
            or any(r["phase"] != "formal" for r in pair)
            or type(left) is not int
            or type(right) is not int
            or right <= left
            or request["execution_state"] not in ("completed", "failed")
        ):
            reasons.add("boundary_window_incomplete:" + key)
            continue
        if pair[0]["read_finished_ns"] > left or pair[1]["read_started_ns"] < right:
            reasons.add("boundary_observation_inside_request:" + key)
            continue
        observations.extend(
            {
                "monotonic_ns": (r["read_started_ns"] + r["read_finished_ns"]) // 2,
                "snapshot": r["environment"],
            }
            for r in pair
        )
        if policy is not None:
            # The baseline has no periodic sampler. Its allowed interval comes
            # exclusively from the frozen bracketing-width policy, not telemetry.
            interval_ms = max(1, int(policy["max_external_interval_seconds"] * 500))
            reduction = reduce_external_cpu(
                [r["cpu"] for r in pair], data["config"]["endpoint"], interval_ms
            )
            window = assess_windows(reduction, [request], policy)["requests"][0]
            windows.append(window)
            if not window["eligible"]:
                reasons.update(key + ":" + reason for reason in window["reasons"])
    environment = assess_environment(
        reads.checked_json("environment.start.json") if "environment.start.json" in sealed else {},
        reads.checked_json("environment.end.json") if "environment.end.json" in sealed else {},
        observations,
        data["config"]["conditions"],
        [],
    )
    reasons.update(environment["reasons"])
    return {
        "definition": "outside_request_environment_qualification.v1",
        "eligible": not reasons,
        "reasons": sorted(reasons),
        "evidence_refs": evidence,
        "request_windows": windows,
        "environment": environment,
        "limitations": [
            "bracketing_cpu_average_not_instantaneous_external_load",
            "environment_changes_between_boundaries_unknown",
            "guards_outside_latency_can_affect_cache_and_request_spacing",
            "no_periodic_sampler_schedule_claim_for_baseline_arm",
        ],
    }
