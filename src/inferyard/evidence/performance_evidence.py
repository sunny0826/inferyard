"""Read-only, source-bound ABBA prerequisite for contextual performance analysis."""

from inferyard.evidence.storage import (
    EvidenceError,
    local_file,
    read_jsonl,
)


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
