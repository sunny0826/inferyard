"""Explicit imports for true token/queue events and process startup timelines."""

from statistics import median

from inferyard.evidence.storage import EvidenceError


def times(events, clock_id):
    result = []
    for event in events:
        value = event.get("monotonic_ns")
        if event.get("clock_id") != clock_id or type(value) is not int or value < 0:
            raise EvidenceError("imported_event_clock_unknown_or_mixed")
        if result and value < result[-1]:
            raise EvidenceError("imported_event_clock_reversed")
        result.append(value)
    return result


def token_intervals(row, clock_id):
    if row.get("source_semantics") != "verified_per_token_events":
        return {"value": None, "reason": "true_token_events_unavailable", "unit": "ms"}
    events, usage = row.get("events", []), row.get("usage_tokens")
    if type(usage) is not int or usage < 0 or len(events) != usage:
        raise EvidenceError("imported_token_event_usage_mismatch")
    if [e.get("token_index") for e in events] != list(range(usage)) or any(
        type(e.get("token_index")) is not int
        or type(e.get("token_id")) is not int
        or e["token_id"] < 0
        for e in events
    ):
        raise EvidenceError("imported_token_event_identity_invalid")
    stamps = times(events, clock_id)
    gaps = [(b - a) / 1e6 for a, b in zip(stamps, stamps[1:], strict=False)]
    return {
        "value": median(gaps) if gaps else None,
        "reason": None if gaps else "fewer_than_two_token_events",
        "intervals_ms": gaps,
        "statistic": "median_true_token_interval",
        "unit": "ms",
    }


def queue_delay(row, clock_id):
    if row.get("source_semantics") != "verified_server_queue_events":
        return {"value": None, "reason": "server_queue_events_unavailable", "unit": "ms"}
    events = row.get("events", [])
    if (
        len(events) != 2
        or [e.get("event") for e in events] != ["enqueued", "execution_started"]
        or len({e.get("request_id") for e in events}) != 1
        or not events[0].get("request_id")
    ):
        raise EvidenceError("imported_queue_event_identity_invalid")
    stamps = times(events, clock_id)
    return {"value": (stamps[1] - stamps[0]) / 1e6, "reason": None, "unit": "ms"}


def startup(row, clock_id):
    if row.get("source_semantics") != "verified_process_lifecycle_events":
        return {
            "ready_seconds": None,
            "first_valid_answer_seconds": None,
            "reason": "startup_events_unavailable",
        }
    events = row.get("events", [])
    if len(events) != 3 or [e.get("event") for e in events] != [
        "process_started",
        "ready",
        "first_valid_answer",
    ]:
        raise EvidenceError("imported_lifecycle_events_incomplete")
    if row.get("start_kind") not in ("cold_process", "warm_process") or row.get(
        "os_cache_state"
    ) not in ("cold", "warm", "unknown"):
        raise EvidenceError("imported_lifecycle_start_policy_unknown")
    if (
        any(
            type(e.get("pid")) is not int
            or e["pid"] <= 0
            or type(e.get("process_start_ticks")) is not int
            or e["process_start_ticks"] <= 0
            for e in events
        )
        or len({(e["pid"], e["process_start_ticks"]) for e in events}) != 1
    ):
        raise EvidenceError("imported_lifecycle_process_changed")
    if events[-1].get("protocol_complete") is not True:
        raise EvidenceError("startup_first_valid_answer_unverified")
    stamps = times(events, clock_id)
    return {
        "ready_seconds": (stamps[1] - stamps[0]) / 1e9,
        "first_valid_answer_seconds": (stamps[2] - stamps[0]) / 1e9,
        "reason": None,
        "start_kind": row["start_kind"],
        "os_cache_state": row["os_cache_state"],
    }


def reduce_import(spec, rows, *, evidence_kind="fixture"):
    if (
        set(spec) != {"definition", "clock_id"}
        or spec["definition"] != "conditional_events.v1"
        or not isinstance(spec["clock_id"], str)
        or not spec["clock_id"]
    ):
        raise EvidenceError("conditional_event_import_spec_invalid")
    if evidence_kind != "fixture" or not isinstance(rows, list) or len(rows) > 128:
        raise EvidenceError("conditional_event_import_is_not_hardware_qualification")
    values = []
    functions = {"tokens": token_intervals, "queue": queue_delay, "startup": startup}
    for row in rows:
        if (
            not isinstance(row, dict)
            or row.get("kind") not in functions
            or len(row.get("events", [])) > 100_000
        ):
            raise EvidenceError("conditional_event_import_row_invalid")
        values.append({"kind": row["kind"], **functions[row["kind"]](row, spec["clock_id"])})
    return {
        "definition": spec["definition"],
        "evidence_kind": evidence_kind,
        "observations": values,
        "hardware_qualified": False,
        "limitations": [
            "import_replay_does_not_authenticate_capture_origin",
            "no_chunk_to_token_conversion",
            "startup_is_not_check_duration",
        ],
    }
