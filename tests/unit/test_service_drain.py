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


@pytest.mark.parametrize(
    "sealed,truncated,observation",
    [
        (False, False, None),
        (True, True, None),
        (True, False, {"mode": "native"}),
    ],
)
def test_unsealed_truncated_and_native_are_not_drain_proof(sealed, truncated, observation):
    assert (
        service_drain(
            [event("idle_observed", None, "idle")],
            sealed=sealed,
            truncated=truncated,
            observation=observation,
        )
        is None
    )


@pytest.mark.parametrize(
    "poisoned,phase,source,allowed",
    [
        (False, "released", "/lab/v1/lifecycle", True),
        (True, "released", "/lab/v1/lifecycle", False),
        (False, "decoding", "/lab/v1/lifecycle", False),
        (False, "released", "/slots:is_processing", False),
    ],
)
def test_lab_proof_uses_validated_lifecycle_and_release(poisoned, phase, source, allowed):
    from tests.unit.test_lab_observation import lifecycle, request_snapshot

    idle = event("idle_observed", "last", "idle")
    idle["data"].update(
        source=source,
        lab_snapshot={
            "lifecycle": lifecycle(1, poisoned=poisoned),
            "request": request_snapshot(
                1, phase=phase, outcome="completed" if phase == "released" else None
            ),
        },
    )
    events = [event("request_started", "last"), idle]
    assert (
        bool(service_drain(events, sealed=True, truncated=False, observation={"mode": "lab"}))
        == allowed
    )
