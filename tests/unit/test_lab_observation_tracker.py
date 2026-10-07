import pytest

from inferyard.adapters.lab_observation import LabProtocolError, ObservationTracker

INSTANCE = "1" * 32
REQUEST = "2" * 32
OTHER_INSTANCE = "3" * 32
STAGES = (
    "accepted_waiting",
    "preparing",
    "prefilling",
    "decoding",
    "output_draining",
    "releasing",
)


def lifecycle(seq, *, poisoned=False, requests=()):
    counts = dict.fromkeys(STAGES, 0)
    items = [
        {"request_id": rid, "phase": phase, "cancel_requested": cancel}
        for rid, phase, cancel in requests
    ]
    for _, phase, _ in requests:
        if type(phase) is str and phase in counts:
            counts[phase] += 1
    return {
        "protocol": "lab_observation.v1",
        "server_instance_id": INSTANCE,
        "snapshot_seq": seq,
        "poisoned": poisoned,
        "active_total": len(items),
        "stages": counts,
        "requests": items,
    }


def request_snapshot(seq, *, phase="decoding", cancel=False, outcome=None, rid=REQUEST):
    return {
        "protocol": "lab_observation.v1",
        "server_instance_id": INSTANCE,
        "snapshot_seq": seq,
        "request_id": rid,
        "phase": phase,
        "cancel_requested": cancel,
        "outcome": outcome,
    }


def reason_of(excinfo):
    assert isinstance(excinfo.value, LabProtocolError)
    return excinfo.value.reason


def test_tracker_accepts_progress_and_repeat_observation():
    tracker = ObservationTracker(INSTANCE)
    first = lifecycle(1, requests=[(REQUEST, "accepted_waiting", False)])
    tracker.accept_lifecycle(first)
    tracker.accept_lifecycle(lifecycle(1, requests=[(REQUEST, "accepted_waiting", False)]))
    tracker.accept_lifecycle(lifecycle(2, requests=[(REQUEST, "decoding", False)]))
    tracker.accept_request(request_snapshot(2, phase="decoding"))
    tracker.accept_request(request_snapshot(2, phase="decoding"))
    tracker.accept_request(request_snapshot(3, phase="releasing", cancel=True))
    tracker.accept_request(request_snapshot(4, phase="released", cancel=True, outcome="cancelled"))


def test_tracker_rejects_seq_regression():
    tracker = ObservationTracker(INSTANCE)
    tracker.accept_lifecycle(lifecycle(2))
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_lifecycle(lifecycle(1))
    assert reason_of(excinfo) == "lab_observation_regressed"


def test_tracker_rejects_same_seq_conflicting_content():
    tracker = ObservationTracker(INSTANCE)
    tracker.accept_lifecycle(lifecycle(2))
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_lifecycle(lifecycle(2, poisoned=True))
    assert reason_of(excinfo) == "lab_observation_regressed"
    tracker.accept_request(request_snapshot(3, phase="decoding"))
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_request(request_snapshot(3, phase="releasing"))
    assert reason_of(excinfo) == "lab_observation_regressed"


def test_tracker_rejects_instance_change():
    tracker = ObservationTracker(INSTANCE)
    payload = lifecycle(1)
    payload["server_instance_id"] = OTHER_INSTANCE
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_lifecycle(payload)
    assert reason_of(excinfo) == "lab_instance_mismatch"


def test_tracker_rejects_phase_regression_across_endpoints():
    tracker = ObservationTracker(INSTANCE)
    tracker.accept_lifecycle(lifecycle(1, requests=[(REQUEST, "decoding", False)]))
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_request(request_snapshot(2, phase="preparing"))
    assert reason_of(excinfo) == "lab_observation_regressed"


def test_tracker_rejects_cancel_flag_rollback():
    tracker = ObservationTracker(INSTANCE)
    tracker.accept_request(request_snapshot(1, phase="decoding", cancel=True))
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_lifecycle(lifecycle(2, requests=[(REQUEST, "decoding", False)]))
    assert reason_of(excinfo) == "lab_observation_regressed"


def test_tracker_rejects_revival_after_release():
    tracker = ObservationTracker(INSTANCE)
    tracker.accept_request(request_snapshot(1, phase="released", outcome="completed"))
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_lifecycle(lifecycle(2, requests=[(REQUEST, "decoding", False)]))
    assert reason_of(excinfo) == "lab_observation_regressed"
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_request(request_snapshot(2, phase="decoding"))
    assert reason_of(excinfo) == "lab_observation_regressed"


def test_tracker_keeps_endpoint_streams_separate():
    tracker = ObservationTracker(INSTANCE)
    tracker.accept_lifecycle(lifecycle(5))
    tracker.accept_request(request_snapshot(3, phase="decoding"))
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_request(request_snapshot(2, phase="decoding"))
    assert reason_of(excinfo) == "lab_observation_regressed"
    tracker.accept_lifecycle(lifecycle(6))
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_lifecycle(lifecycle(4))
    assert reason_of(excinfo) == "lab_observation_regressed"


def test_tracker_rejects_invalid_snapshot_structure():
    tracker = ObservationTracker(INSTANCE)
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_lifecycle({"protocol": "lab_observation.v1"})
    assert reason_of(excinfo) == "lab_invalid_lifecycle"
    with pytest.raises(LabProtocolError) as excinfo:
        tracker.accept_request(request_snapshot(1, phase="queued"))
    assert reason_of(excinfo) == "lab_invalid_request"


def test_tracker_rejects_bad_constructor_instance_id():
    with pytest.raises(LabProtocolError) as excinfo:
        ObservationTracker("not-an-id")
    assert reason_of(excinfo) == "lab_instance_mismatch"
