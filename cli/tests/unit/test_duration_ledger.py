from copy import deepcopy

import pytest

from inferyard.evidence.duration_ledger import duration_summary
from inferyard.evidence.storage import EvidenceError

P = {
    "duration_seconds": 1,
    "window_seconds": 1,
    "drain_timeout_seconds": 1,
    "max_requests": 5,
    "min_completed_per_case_per_window": 1,
}


def evidence():
    events = [
        {
            "event_type": "duration_started",
            "seq": 1,
            "phase": "formal",
            "request_id": None,
            "monotonic_ns": 0,
            "data": {
                "start_ns": 0,
                "admission_deadline_ns": 10**9,
                "drain_deadline_ns": 2 * 10**9,
                "request_limit": 5,
            },
        },
        {
            "event_type": "request_started",
            "seq": 2,
            "phase": "formal",
            "request_id": "r1",
            "monotonic_ns": 1,
            "data": {},
        },
        {
            "event_type": "duration_closed",
            "seq": 4,
            "phase": "formal",
            "request_id": None,
            "monotonic_ns": 10**9,
            "data": {
                "closed_ns": 10**9,
                "returned_requests": 1,
                "completed_requests": 1,
                "reason": "duration_elapsed",
            },
        },
    ]
    rows = [
        {
            "request_id": "r1",
            "case_id": "a",
            "execution_state": "completed",
            "t_send_ns": 1,
            "t_terminal_ns": 100,
        }
    ]
    return events, rows


def test_full_window_and_probe_coverage_are_separate():
    events, rows = evidence()
    result = duration_summary(P, ["a", "b"], events, rows)
    assert result["window_completed"]
    assert not result["probe_coverage_complete"]
    assert not duration_summary(P, ["a"], events[:-1], rows)["window_completed"]


@pytest.mark.parametrize(
    "fault", ["deadline", "late_start", "early_close", "late_response", "duplicate"]
)
def test_forged_duration_boundaries_rejected(fault):
    events, rows = evidence()
    if fault == "deadline":
        events[0]["data"]["admission_deadline_ns"] += 1
    elif fault == "late_start":
        events[1]["monotonic_ns"] = 10**9
    elif fault == "early_close":
        events[2]["monotonic_ns"] = events[2]["data"]["closed_ns"] = 99
    elif fault == "late_response":
        events.append({"event_type": "request_finished", "seq": 5, "request_id": "r1"})
    else:
        events.insert(1, deepcopy(events[0]))
    with pytest.raises(EvidenceError):
        duration_summary(P, ["a"], events, rows)
