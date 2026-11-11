import json
from pathlib import Path

import pytest

from inferyard.contracts.lab_observation import (
    LabProtocolError,
    is_idle,
    parse_cancel,
    parse_lifecycle,
    parse_request,
)

FIXTURES = Path(__file__).resolve().parent.parent / "fixtures" / "lab_observation"
INSTANCE = "1" * 32
REQUEST = "2" * 32
OTHER_INSTANCE = "3" * 32
OTHER_REQUEST = "4" * 32
STAGES = (
    "accepted_waiting",
    "preparing",
    "prefilling",
    "decoding",
    "output_draining",
    "releasing",
)


def fixture(name):
    return (FIXTURES / name).read_bytes()


def as_bytes(obj):
    return json.dumps(obj).encode()


def lifecycle(seq, *, poisoned=False, phase_counts=None, requests=()):
    counts = dict.fromkeys(STAGES, 0)
    items = [
        {"request_id": rid, "phase": phase, "cancel_requested": cancel}
        for rid, phase, cancel in requests
    ]
    if phase_counts is not None:
        counts.update(phase_counts)
        total = sum(counts.values())
    else:
        for _, phase, _ in requests:
            if type(phase) is str and phase in counts:
                counts[phase] += 1
        total = len(items)
    return {
        "protocol": "lab_observation.v1",
        "server_instance_id": INSTANCE,
        "snapshot_seq": seq,
        "poisoned": poisoned,
        "active_total": total,
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


def cancel_response(disposition="accepted"):
    return {
        "protocol": "lab_observation.v1",
        "server_instance_id": INSTANCE,
        "request_id": REQUEST,
        "disposition": disposition,
    }


def reason_of(excinfo):
    return excinfo.value.reason


def test_parse_lifecycle_rejects_instance_mismatch():
    with pytest.raises(LabProtocolError) as excinfo:
        parse_lifecycle(fixture("lifecycle.idle.json"), expected_instance_id=OTHER_INSTANCE)
    assert reason_of(excinfo) == "lab_instance_mismatch"


@pytest.mark.parametrize(
    "payload",
    [
        lifecycle(0),
        lifecycle(1, phase_counts={"decoding": 1}, requests=[]),
        lifecycle(1, phase_counts={"decoding": 2}, requests=[(REQUEST, "decoding", False)]),
        lifecycle(1, phase_counts={}, requests=[(REQUEST, "decoding", False)]),
        lifecycle(1, requests=[(REQUEST, "released", False)]),
        lifecycle(1, requests=[(REQUEST, "decoding", False), (REQUEST, "decoding", False)]),
        lifecycle(1, requests=[("z" * 32, "decoding", False)]),
    ],
)
def test_parse_lifecycle_rejects_inconsistent(payload):
    with pytest.raises(LabProtocolError) as excinfo:
        parse_lifecycle(as_bytes(payload), expected_instance_id=INSTANCE)
    assert reason_of(excinfo) == "lab_invalid_lifecycle"


def test_parse_request_rejects_request_mismatch():
    with pytest.raises(LabProtocolError) as excinfo:
        parse_request(
            fixture("request.active.json"),
            expected_instance_id=INSTANCE,
            expected_request_id=OTHER_REQUEST,
        )
    assert reason_of(excinfo) == "lab_request_mismatch"


@pytest.mark.parametrize(
    "payload",
    [
        request_snapshot(1, outcome="completed"),
        request_snapshot(1, phase="released"),
        request_snapshot(1, phase="released", outcome="lost"),
        request_snapshot(1, phase="queued"),
        request_snapshot(0),
        request_snapshot(1, cancel=1),
    ],
)
def test_parse_request_rejects_invalid(payload):
    with pytest.raises(LabProtocolError) as excinfo:
        parse_request(as_bytes(payload), expected_instance_id=INSTANCE, expected_request_id=REQUEST)
    assert reason_of(excinfo) == "lab_invalid_request"


def test_parse_cancel_rejects_mismatched_ids():
    with pytest.raises(LabProtocolError) as excinfo:
        parse_cancel(
            as_bytes(cancel_response()),
            expected_instance_id=OTHER_INSTANCE,
            expected_request_id=REQUEST,
        )
    assert reason_of(excinfo) == "lab_instance_mismatch"
    with pytest.raises(LabProtocolError) as excinfo:
        parse_cancel(
            as_bytes(cancel_response()),
            expected_instance_id=INSTANCE,
            expected_request_id=OTHER_REQUEST,
        )
    assert reason_of(excinfo) == "lab_request_mismatch"


def test_is_idle_true_only_for_clean_empty_snapshot():
    assert is_idle(json.loads(fixture("lifecycle.idle.json"))) is True
    assert is_idle(json.loads(fixture("lifecycle.poisoned.json"))) is False
    assert is_idle(json.loads(fixture("lifecycle.active.json"))) is False
