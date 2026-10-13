import asyncio

import pytest

from inferyard.evidence.storage import EvidenceError
from inferyard.runtime.duration_driver import run_duration


def protocol(**kwargs):
    return {
        "kind": "duration",
        "case_ids": ["a", "b"],
        "duration_seconds": 1,
        "drain_timeout_seconds": 1,
        "max_requests": 10,
        **kwargs,
    }


def test_cycles_cases_and_allows_request_to_drain_after_window():
    now = 100
    calls, events = [], []

    async def request(index, case, admission, drain):
        nonlocal now
        assert now < admission < drain
        calls.append((index, case))
        now += 400_000_000
        return {"execution_state": "completed"}

    result = asyncio.run(
        run_duration(
            protocol(), ["a", "b"], request, lambda *args: events.append(args), clock=lambda: now
        )
    )
    assert calls == [(0, "a"), (1, "b"), (2, "a")]
    assert result["window_completed"]
    assert result["completed_requests"] == 3
    assert events[0][0] == "duration_started" and events[-1][0] == "duration_closed"


def test_request_budget_exhaustion_does_not_claim_full_window():
    now = 0

    async def request(*args):
        nonlocal now
        now += 10
        return {"execution_state": "failed"}

    result = asyncio.run(
        run_duration(
            protocol(max_requests=2), ["a", "b"], request, lambda *args: None, clock=lambda: now
        )
    )
    assert result["returned_requests"] == 2
    assert result["completed_requests"] == 0
    assert not result["window_completed"]
    assert result["reason"] == "request_limit_reached"


def test_admission_check_after_idle_can_decline_without_attempt():
    now = 0

    async def request(index, case, admission, drain):
        nonlocal now
        now = admission
        return None

    result = asyncio.run(
        run_duration(protocol(), ["a", "b"], request, lambda *args: None, clock=lambda: now)
    )
    assert result["returned_requests"] == 0
    assert result["window_completed"]


def test_premature_skip_is_integrity_error():
    async def request(*args):
        return None

    with pytest.raises(EvidenceError, match="skipped"):
        asyncio.run(
            run_duration(protocol(), ["a", "b"], request, lambda *args: None, clock=lambda: 0)
        )


def test_inflight_request_cancelled_at_drain_deadline():
    cancelled = []

    async def request(*args):
        try:
            await asyncio.sleep(10)
        finally:
            cancelled.append(True)

    result = asyncio.run(
        run_duration(
            protocol(duration_seconds=0.01, drain_timeout_seconds=0.01),
            ["a", "b"],
            request,
            lambda *args: None,
        )
    )
    assert cancelled == [True]
    assert result["reason"] == "drain_deadline_exceeded"
    assert not result["window_completed"]


def test_user_cancel_propagates_and_records_interruption():
    events = []

    async def request(*args):
        raise asyncio.CancelledError()

    with pytest.raises(asyncio.CancelledError):
        asyncio.run(
            run_duration(protocol(), ["a", "b"], request, lambda *args: events.append(args))
        )
    assert events[-1][1]["reason"] == "interrupted"
