"""Reuse needs a sealed final idle observation, independently of quality/scope."""

import pytest

from inferyard.evidence.service_drain import service_drain


def event(kind, key, state=None):
    return dict(
        event_type=kind,
        request_id=key,
        seq=5,
        data={"state": state, "source": "/slots:is_processing"},
    )


@pytest.mark.parametrize(
    "tail,allowed",
    [
        ([], True),
        ([event("request_started", "next")], False),
        ([event("idle_observed", "last", "busy")], False),
        ([event("idle_observed", "other", "idle")], False),
        ([event("run_stopped", None)], True),
        ([event("request_scored", "last")], True),
    ],
)
def test_only_final_idle_after_latest_request_proves_drain(tail, allowed):
    events = [event("request_started", "last"), event("idle_observed", "last", "idle")]
    assert (
        bool(service_drain(events + tail, sealed=True, truncated=False, observation=None))
        == allowed
    )
