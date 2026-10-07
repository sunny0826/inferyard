import json
from pathlib import Path

import pytest

from inferyard.adapters.lab_observation import (
    LabProtocolError,
    is_idle,
    parse_cancel,
    parse_identity,
    parse_lifecycle,
    parse_request,
)

FIXTURES = Path(__file__).resolve().parent.parent / "fixtures" / "lab_observation"
INSTANCE = "1" * 32
REQUEST = "2" * 32
OTHER_INSTANCE = "3" * 32
OTHER_REQUEST = "4" * 32
SHA_A = "a" * 64
SHA_B = "b" * 64
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


def identity(**overrides):
    data = {
        "protocol": "lab_observation.v1",
        "server_instance_id": INSTANCE,
        "engine": "ninfer",
        "build_id": "lab-patched-build",
        "model": {"kind": "ninfer", "sha256": SHA_A, "bytes": 100},
        "template_sha256": SHA_B,
        "capabilities": {
            "lifecycle": True,
            "request_cancel": True,
            "request_id": True,
            "token_budget": False,
            "effective_parameters": False,
        },
    }
    data.update(overrides)
    return data


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
    assert isinstance(excinfo.value, LabProtocolError)
    return excinfo.value.reason


# --- identity ------------------------------------------------------------


def test_parse_identity_ninfer_fixture():
    data = parse_identity(fixture("identity.ninfer.json"), expected_engine="ninfer")
    assert data["engine"] == "ninfer"
    assert data["model"]["kind"] == "ninfer"
    assert data["template_sha256"] == SHA_B


def test_parse_identity_kvmem_fixture_template_null():
    data = parse_identity(fixture("identity.kvmem.json"), expected_engine="kvmem")
    assert data["model"]["kind"] == "gguf"
    assert data["template_sha256"] is None


@pytest.mark.parametrize(
    "payload",
    [
        identity(engine="kvmem"),
        identity(protocol="lab_observation.v2"),
        identity(server_instance_id="A" * 32),
        identity(server_instance_id="1" * 31),
        identity(build_id=""),
        identity(build_id="x" * 129),
        identity(build_id="补丁"),
        identity(model={"kind": "gguf", "sha256": SHA_A, "bytes": 100}),
        identity(model={"kind": "ninfer", "sha256": SHA_A.upper(), "bytes": 100}),
        identity(model={"kind": "ninfer", "sha256": SHA_A, "bytes": 0}),
        identity(model={"kind": "ninfer", "sha256": SHA_A, "bytes": True}),
        identity(template_sha256="zz"),
    ],
)
def test_parse_identity_rejects_invalid(payload):
    with pytest.raises(LabProtocolError) as excinfo:
        parse_identity(as_bytes(payload), expected_engine="ninfer")
    assert reason_of(excinfo) == "lab_invalid_identity"


@pytest.mark.parametrize("field", ["lifecycle", "token_budget"])
def test_parse_identity_rejects_non_bool_capability(field):
    payload = identity()
    payload["capabilities"][field] = 1
    with pytest.raises(LabProtocolError) as excinfo:
        parse_identity(as_bytes(payload), expected_engine="ninfer")
    assert reason_of(excinfo) == "lab_invalid_identity"


@pytest.mark.parametrize(
    "mutate",
    [
        lambda d: d.update(vendor=1),
        lambda d: d.pop("engine"),
        lambda d: d["model"].update(extra=1),
        lambda d: d["capabilities"].pop("lifecycle"),
    ],
)
def test_parse_identity_rejects_non_exact_key_sets(mutate):
    payload = identity()
    mutate(payload)
    with pytest.raises(LabProtocolError) as excinfo:
        parse_identity(as_bytes(payload), expected_engine="ninfer")
    assert reason_of(excinfo) == "lab_invalid_identity"


def test_parse_identity_rejects_unknown_expected_engine():
    with pytest.raises(LabProtocolError) as excinfo:
        parse_identity(as_bytes(identity()), expected_engine="llama_cpp")
    assert reason_of(excinfo) == "lab_invalid_identity"


@pytest.mark.parametrize(
    "raw",
    [
        b'{"protocol": "lab_observation.v1", "protocol": "lab_observation.v1"}',
        b'{"protocol": "lab_observation.v1", "engine": NaN}',
        b'\xff\xfe{"protocol": 1}',
        b"[1, 2]",
    ],
)
def test_parse_identity_rejects_broken_wire(raw):
    with pytest.raises(LabProtocolError) as excinfo:
        parse_identity(raw, expected_engine="ninfer")
    assert reason_of(excinfo) == "lab_invalid_identity"


# --- lifecycle -----------------------------------------------------------


def test_parse_lifecycle_fixtures():
    idle = parse_lifecycle(fixture("lifecycle.idle.json"), expected_instance_id=INSTANCE)
    assert idle["active_total"] == 0
    active = parse_lifecycle(fixture("lifecycle.active.json"), expected_instance_id=INSTANCE)
    assert active["requests"][0]["phase"] == "decoding"


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


def test_parse_lifecycle_rejects_sixteen_plus_requests():
    payload = lifecycle(1, requests=[(f"{i:032x}", "decoding", False) for i in range(17)])
    with pytest.raises(LabProtocolError) as excinfo:
        parse_lifecycle(as_bytes(payload), expected_instance_id=INSTANCE)
    assert reason_of(excinfo) == "lab_invalid_lifecycle"


def test_parse_lifecycle_accepts_sixteen_requests():
    payload = lifecycle(
        1,
        phase_counts={"decoding": 16},
        requests=[(f"{i:032x}", "decoding", False) for i in range(16)],
    )
    data = parse_lifecycle(as_bytes(payload), expected_instance_id=INSTANCE)
    assert data["active_total"] == 16


@pytest.mark.parametrize(
    "mutate",
    [
        lambda d: d["stages"].update(decoding=True),
        lambda d: d["stages"].pop("releasing"),
        lambda d: d["stages"].update(vendor=0),
        lambda d: d.update(poisoned=1),
        lambda d: d.update(snapshot_seq=True),
        lambda d: d["requests"].append({"request_id": "5" * 32, "phase": "decoding"}),
    ],
)
def test_parse_lifecycle_rejects_type_violations(mutate):
    payload = lifecycle(1)
    mutate(payload)
    with pytest.raises(LabProtocolError) as excinfo:
        parse_lifecycle(as_bytes(payload), expected_instance_id=INSTANCE)
    assert reason_of(excinfo) == "lab_invalid_lifecycle"


# --- request -------------------------------------------------------------


def test_parse_request_fixtures():
    active = parse_request(
        fixture("request.active.json"), expected_instance_id=INSTANCE, expected_request_id=REQUEST
    )
    assert active["outcome"] is None
    released = parse_request(
        fixture("request.released.json"), expected_instance_id=INSTANCE, expected_request_id=REQUEST
    )
    assert released["outcome"] == "cancelled"


def test_parse_request_rejects_instance_mismatch():
    with pytest.raises(LabProtocolError) as excinfo:
        parse_request(
            fixture("request.active.json"),
            expected_instance_id=OTHER_INSTANCE,
            expected_request_id=REQUEST,
        )
    assert reason_of(excinfo) == "lab_instance_mismatch"


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


# --- cancel --------------------------------------------------------------


def test_parse_cancel_accepted_fixture():
    data = parse_cancel(
        fixture("cancel.accepted.json"), expected_instance_id=INSTANCE, expected_request_id=REQUEST
    )
    assert data["disposition"] == "accepted"


def test_parse_cancel_error_fixture():
    data = parse_cancel(
        fixture("cancel.error.json"), expected_instance_id=INSTANCE, expected_request_id=REQUEST
    )
    assert data == {"protocol": "lab_observation.v1", "error": "request_not_found"}


@pytest.mark.parametrize(
    "payload",
    [
        cancel_response("rejected"),
        cancel_response() | {"extra": 1},
        {"protocol": "lab_observation.v1", "error": "unknown_error"},
        {"protocol": "lab_observation.v2", "error": "request_not_found"},
        {"error": "request_not_found"},
    ],
)
def test_parse_cancel_rejects_invalid(payload):
    with pytest.raises(LabProtocolError) as excinfo:
        parse_cancel(as_bytes(payload), expected_instance_id=INSTANCE, expected_request_id=REQUEST)
    assert reason_of(excinfo) == "lab_invalid_cancel"


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


# --- is_idle -------------------------------------------------------------


def test_is_idle_true_only_for_clean_empty_snapshot():
    assert is_idle(json.loads(fixture("lifecycle.idle.json"))) is True
    assert is_idle(json.loads(fixture("lifecycle.poisoned.json"))) is False
    assert is_idle(json.loads(fixture("lifecycle.active.json"))) is False


def test_is_idle_revalidates_input():
    with pytest.raises(LabProtocolError) as excinfo:
        is_idle({"protocol": "lab_observation.v1"})
    assert reason_of(excinfo) == "lab_invalid_lifecycle"


def test_is_idle_does_not_mutate_input():
    snapshot = json.loads(fixture("lifecycle.active.json"))
    original = json.loads(fixture("lifecycle.active.json"))
    is_idle(snapshot)
    assert snapshot == original


# --- enum type safety (review round 1) ---------------------------------------


@pytest.mark.parametrize("bad", [[], {}, 1, True, 1.5])
def test_parse_cancel_rejects_non_string_enum_values(bad):
    with pytest.raises(LabProtocolError) as excinfo:
        parse_cancel(
            as_bytes({"protocol": "lab_observation.v1", "error": bad}),
            expected_instance_id=INSTANCE,
            expected_request_id=REQUEST,
        )
    assert reason_of(excinfo) == "lab_invalid_cancel"
    with pytest.raises(LabProtocolError) as excinfo:
        parse_cancel(
            as_bytes(cancel_response(bad)),
            expected_instance_id=INSTANCE,
            expected_request_id=REQUEST,
        )
    assert reason_of(excinfo) == "lab_invalid_cancel"


@pytest.mark.parametrize("bad", [[], {}, 1, True])
def test_parse_request_rejects_non_string_enum_values(bad):
    with pytest.raises(LabProtocolError) as excinfo:
        parse_request(
            as_bytes(request_snapshot(1, phase="released", outcome=bad)),
            expected_instance_id=INSTANCE,
            expected_request_id=REQUEST,
        )
    assert reason_of(excinfo) == "lab_invalid_request"
    with pytest.raises(LabProtocolError) as excinfo:
        parse_request(
            as_bytes(request_snapshot(1, phase=bad)),
            expected_instance_id=INSTANCE,
            expected_request_id=REQUEST,
        )
    assert reason_of(excinfo) == "lab_invalid_request"


def test_parse_lifecycle_rejects_non_string_phase():
    payload = lifecycle(1, requests=[(REQUEST, [], False)])
    with pytest.raises(LabProtocolError) as excinfo:
        parse_lifecycle(as_bytes(payload), expected_instance_id=INSTANCE)
    assert reason_of(excinfo) == "lab_invalid_lifecycle"


def test_parse_identity_rejects_non_string_engine_and_kind():
    with pytest.raises(LabProtocolError) as excinfo:
        parse_identity(as_bytes(identity(engine=[])), expected_engine="ninfer")
    assert reason_of(excinfo) == "lab_invalid_identity"
    payload = identity(model={"kind": [], "sha256": SHA_A, "bytes": 100})
    with pytest.raises(LabProtocolError) as excinfo:
        parse_identity(as_bytes(payload), expected_engine="ninfer")
    assert reason_of(excinfo) == "lab_invalid_identity"
