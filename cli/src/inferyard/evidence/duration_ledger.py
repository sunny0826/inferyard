"""Validate frozen window boundaries against durable request admission events."""

from inferyard.analysis.duration_windows import summarize_windows
from inferyard.evidence.storage import EvidenceError


def duration_summary(protocol, order, events, requests):
    starts = [e for e in events if e["event_type"] == "duration_started"]
    ends = [e for e in events if e["event_type"] == "duration_closed"]
    admissions = [
        e for e in events if e["event_type"] == "request_started" and e["phase"] == "formal"
    ]
    if len(starts) > 1 or len(ends) > 1 or (ends and not starts) or (admissions and not starts):
        raise EvidenceError("invalid_duration_window_sequence")
    if not starts:
        return {"window_completed": False, "reason": "window_not_started", "windows": []}
    start = starts[0]["data"]
    left = start["start_ns"]
    deadline = left + int(protocol["duration_seconds"] * 1e9)
    drain = deadline + int(protocol["drain_timeout_seconds"] * 1e9)
    if (
        starts[0]["monotonic_ns"] != left
        or start["admission_deadline_ns"] != deadline
        or start["drain_deadline_ns"] != drain
        or start["request_limit"] != protocol["max_requests"]
    ):
        raise EvidenceError("duration_window_differs_from_protocol")
    if len(admissions) > protocol["max_requests"]:
        raise EvidenceError("duration_request_limit_exceeded")
    if any(
        e["seq"] <= starts[0]["seq"] or not left <= e["monotonic_ns"] < deadline for e in admissions
    ):
        raise EvidenceError("duration_admission_outside_window")
    result = {
        **start,
        "admitted_requests": len(admissions),
        "window_completed": False,
        "reason": "window_close_missing",
        **summarize_windows(protocol, order, left, requests),
    }
    result["probe_coverage_complete"] = bool(result["windows"]) and all(
        c["median_latency_ns"] is not None for w in result["windows"] for c in w["cases"]
    )
    if not ends:
        return result
    end = ends[0]
    data = end["data"]
    if (
        end["seq"] <= starts[0]["seq"]
        or data["closed_ns"] != end["monotonic_ns"]
        or any(e["seq"] >= end["seq"] for e in admissions)
        or data["returned_requests"] > len(admissions)
        or data["completed_requests"] > data["returned_requests"]
    ):
        raise EvidenceError("invalid_duration_close")
    result.update(reason=data["reason"], closed_ns=data["closed_ns"])
    if data["reason"] == "duration_elapsed":
        keys = {e["request_id"] for e in admissions}
        if any(e["request_id"] in keys and e["seq"] >= end["seq"] for e in events):
            raise EvidenceError("duration_response_after_close")
        if (
            not deadline <= data["closed_ns"] <= drain
            or data["returned_requests"] != len(admissions)
            or data["completed_requests"]
            != sum(r["execution_state"] == "completed" for r in requests)
            or any(r["execution_state"] not in ("completed", "failed") for r in requests)
        ):
            raise EvidenceError("duration_success_without_complete_window")
        result["window_completed"] = True
    return result
