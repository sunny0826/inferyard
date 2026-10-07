"""Read-only, source-bound ABBA prerequisite for contextual performance analysis."""

from inferyard.evidence.storage import (
    EvidenceError,
    local_file,
    read_json,
    read_jsonl,
    sha256_file,
)
from inferyard.runtime.overhead_runner import read_overhead


def clock_span(root):
    records, issues = read_jsonl(local_file(root, "request-environment.jsonl"))
    events, event_issues = read_jsonl(local_file(root, "events.jsonl"))
    if issues or event_issues:
        raise EvidenceError("performance_chronology_log_invalid")
    boots = {r.get("cpu", {}).get("boot_id") for r in records}
    boot = next(iter(boots)) if len(boots) == 1 else None
    known = isinstance(boot, str) and bool(boot) and bool(events)
    return {
        "boot_id": boot if known else None,
        "start_ns": min((e["monotonic_ns"] for e in events), default=None),
        "end_ns": max((e["monotonic_ns"] for e in events), default=None),
    }


def load_performance_evidence(root, target):
    result = read_overhead(root, target=target)
    protocol = read_json(local_file(root, "protocol.json"))
    reasons = []
    if protocol["kind"] != "collector_overhead_abba.v3":
        reasons.append("outside_request_baseline_protocol_required")
    if not result["passed"]:
        reasons.append("overhead_tolerance_not_passed")
    if not result["target_binding"]["applicable"]:
        reasons.extend(result["target_binding"]["reasons"])
    if not result["environment_binding"]["eligible"]:
        reasons.extend(result["environment_binding"]["reasons"])
    chronology = []
    if protocol["kind"] == "collector_overhead_abba.v3":
        target_clock = clock_span(target)
        for entry in read_json(local_file(root, "trials.json")):
            arm = clock_span(local_file(root, entry["path"]))
            valid = (
                target_clock["boot_id"] is not None
                and target_clock["boot_id"] == arm["boot_id"]
                and arm["end_ns"] is not None
                and target_clock["start_ns"] is not None
                and arm["end_ns"] < target_clock["start_ns"]
            )
            chronology.append({"run_id": entry["run_id"], "precedes_target": valid})
        if len(chronology) != 4 or not all(r["precedes_target"] for r in chronology):
            reasons.append("preflight_not_before_target_in_same_boot")
    from inferyard.evidence.total_control_binding import load_total_binding

    total = load_total_binding(root, target, clock_span(target))
    if not total["eligible"]:
        reasons.extend(total["reasons"])
    if (
        total.get("tolerance_ratio") is not None
        and total["tolerance_ratio"] != protocol["tolerance_ratio"]
    ):
        reasons.append("incremental_and_full_observer_tolerances_differ")
    return {
        "eligible": not reasons,
        "reasons": sorted(set(reasons)),
        "target_run_id": result["target_binding"]["target_run_id"],
        "target_manifest_sha256": result["target_binding"]["target_manifest_sha256"],
        "tolerance_ratio": protocol["tolerance_ratio"],
        "chronology": chronology,
        "source": {
            "path": str(root.resolve()),
            "protocol_sha256": protocol["protocol_sha256"],
            "trials_sha256": sha256_file(local_file(root, "trials.json")),
        },
        "assessment": result,
        "total_observer_binding": total,
    }
