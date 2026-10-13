"""Audit recorded observer wall intervals; persistence and total perturbation stay unknown."""

from inferyard.evidence.storage import EvidenceError, read_jsonl


def union_ns(intervals):
    ordered = sorted(intervals)
    total = 0
    left = right = None
    for start, end in ordered:
        if type(start) is not int or type(end) is not int or start < 0 or end < start:
            raise EvidenceError("invalid_observer_cost_interval")
        if right is None or start > right:
            if right is not None:
                total += right - left
            left, right = start, end
        else:
            right = max(right, end)
    return total + (right - left if right is not None else 0)


def intervals(root, filename, keys):
    if not (root / filename).is_file():
        return None
    rows, issues = read_jsonl(root / filename)
    if issues:
        raise EvidenceError("invalid_observer_cost_log")
    result = [(r[keys[0]], r[keys[1]]) for r in rows]
    union_ns(result)
    return result


def audit(root, data):
    """Caller must read_trial first, to verify the sealed source and clock context."""
    components = {
        "safety_guard_reads": intervals(
            root, "safety-checks.jsonl", ("check_started_ns", "check_finished_ns")
        ),
        "boundary_environment_reads": intervals(
            root, "request-environment.jsonl", ("read_started_ns", "read_finished_ns")
        ),
        "resource_and_sensor_reads": intervals(
            root, "memory.jsonl", ("read_started_ns", "read_finished_ns")
        ),
    }
    all_intervals = [pair for pairs in components.values() if pairs for pair in pairs]
    requests = []
    for row in data["requests"]:
        start, end = row["t_send_ns"], row["t_terminal_ns"]
        if start is None or end is None or end <= start:
            requests.append({"case_id": row["case_id"], "recorded_wall_ms": None})
            continue
        clipped = [(max(a, start), min(b, end)) for a, b in all_intervals if a < end and b > start]
        value = union_ns(clipped) if all_intervals else None
        requests.append(
            {
                "case_id": row["case_id"],
                "recorded_wall_ms": value / 1e6 if value is not None else None,
                "request_duration_ms": (end - start) / 1e6,
                "recorded_interval_fraction": value / (end - start) if value is not None else None,
            }
        )
    return {
        "definition": "recorded_observer_read_wall_intervals.v1",
        "run_id": data["run"]["run_id"],
        "components": {
            name: {
                "available": pairs is not None and bool(pairs),
                "records": len(pairs) if pairs is not None else None,
                "union_wall_ms": union_ns(pairs) / 1e6 if pairs else None,
            }
            for name, pairs in components.items()
        },
        "all_recorded_intervals_union_ms": union_ns(all_intervals) / 1e6 if all_intervals else None,
        "formal_requests": requests,
        "total_observation_cost_qualified": False,
        "limitations": [
            "recorded_wall_intervals_not_cpu_time_or_causal_latency_cost",
            "json_serialization_persistence_fsync_and_some_environment_reads_not_timed",
            "boundary_reads_can_change_spacing_and_cache_outside_request_timing",
            "overlapping_intervals_counted_once",
        ],
    }
