"""Bounded serial duration driver, independent of persistence and transport.

The request callback must re-check admission_deadline_ns immediately before
sending, including after its pre-request idle check, and return None if expired.
Returning None must not create a durable request attempt.
"""

import asyncio
import time

from inferyard.evidence.storage import EvidenceError


async def run_duration(protocol, case_order, request, emit, *, clock=time.monotonic_ns):
    if protocol["kind"] != "duration" or not case_order:
        raise EvidenceError("duration_protocol_required")
    if len(set(case_order)) != len(case_order) or set(case_order) != set(protocol["case_ids"]):
        raise EvidenceError("duration_case_order_mismatch")
    start = clock()
    deadline = start + int(protocol["duration_seconds"] * 1e9)
    drain_deadline = deadline + int(protocol["drain_timeout_seconds"] * 1e9)
    if deadline <= start or drain_deadline <= deadline:
        raise EvidenceError("duration_below_clock_resolution")
    state = {
        "start_ns": start,
        "admission_deadline_ns": deadline,
        "drain_deadline_ns": drain_deadline,
        "request_limit": protocol["max_requests"],
    }
    emit("duration_started", state)
    completed = started = 0
    reason = "interrupted"
    try:
        while clock() < deadline:
            if started >= protocol["max_requests"]:
                reason = "request_limit_reached"
                break
            remaining = max(0, drain_deadline - clock()) / 1e9
            timeout = asyncio.timeout(remaining)
            try:
                async with timeout:
                    result = await request(
                        started, case_order[started % len(case_order)], deadline, drain_deadline
                    )
            except TimeoutError:
                if not timeout.expired():
                    raise
                reason = "drain_deadline_exceeded"
                break
            if result is None:
                if clock() < deadline:
                    raise EvidenceError("duration_request_skipped_before_deadline")
                reason = "duration_elapsed"
                break
            started += 1
            completed += result["execution_state"] == "completed"
            if clock() > drain_deadline:
                reason = "drain_deadline_exceeded"
                break
        else:
            reason = "duration_elapsed"
    finally:
        # started counts returned attempts, not a cancellation in flight; durable
        # request_started events are the authoritative attempted-request count.
        emit(
            "duration_closed",
            {
                "reason": reason,
                "returned_requests": started,
                "completed_requests": completed,
                "closed_ns": clock(),
            },
        )
    return {
        **state,
        "reason": reason,
        "returned_requests": started,
        "completed_requests": completed,
        "window_completed": reason == "duration_elapsed" and clock() >= deadline,
    }
