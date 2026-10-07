from copy import deepcopy

import pytest

from inferyard.evidence.storage import EvidenceError
from inferyard.runtime.collector_overhead import ORDER, assess_overhead, freeze_protocol


def protocol(**kwargs):
    return freeze_protocol(
        **{
            "case_ids": ["a", "b"],
            "interval_ms": 100,
            "tolerance_ratio": 0.05,
            "max_wall_seconds": 60,
            "config_sha256": "a" * 64,
            "bundle_sha256": "b" * 64,
            "tool_source_sha256": "c" * 64,
            **kwargs,
        }
    )


def trials(p, durations=(100, 104, 104, 100)):
    return [
        {
            "mode": mode,
            "completed": True,
            "protocol_sha256": p["protocol_sha256"],
            "requests": [
                {
                    "case_id": case,
                    "duration_ns": duration,
                    "execution_state": "completed",
                    "output_identity": "same",
                }
                for case in p["case_ids"]
            ],
        }
        for mode, duration in zip(ORDER, durations, strict=True)
    ]


def test_exact_tolerance_and_independent_pairs():
    p = protocol()
    assert assess_overhead(p, trials(p, (100, 105, 105, 100)))["passed"]
    result = assess_overhead(p, trials(p, (100, 110, 90, 100)))
    assert not result["passed"]
    assert result["max_absolute_pair_median_change"] == 0.1


@pytest.mark.parametrize(
    "field,value",
    [
        ("tolerance_ratio", float("nan")),
        ("tolerance_ratio", True),
        ("max_wall_seconds", 0),
        ("interval_ms", 0),
        ("case_ids", ["a", "a"]),
        ("tool_source_sha256", "unknown"),
    ],
)
def test_freeze_rejects_invalid_inputs(field, value):
    with pytest.raises(EvidenceError):
        protocol(**{field: value})


def test_partial_failed_or_different_work_is_not_pass():
    p = protocol()
    assert not assess_overhead(p, trials(p)[:3])["passed"]
    for key, value in [
        ("execution_state", "failed"),
        ("duration_ns", 0),
        ("output_identity", "different"),
        ("case_id", "wrong"),
    ]:
        rows = trials(p)
        rows[1]["requests"][0][key] = value
        assert not assess_overhead(p, rows)["passed"]


def test_tampered_protocol_or_order_rejected():
    p = protocol()
    changed = deepcopy(p)
    changed["tolerance_ratio"] = 0.9
    with pytest.raises(EvidenceError, match="protocol"):
        assess_overhead(changed, trials(p))
    rows = trials(p)
    rows[0]["mode"] = "on"
    with pytest.raises(EvidenceError, match="binding"):
        assess_overhead(p, rows)


def test_common_observer_protocol_is_versioned_and_cannot_be_relabelled():
    from inferyard.runtime.collector_overhead import validate_protocol

    old, new = protocol(), protocol(common_observer=True)
    assert old["kind"] == "collector_overhead_abba.v1"
    assert "observer" not in old
    assert new["kind"] == "collector_overhead_abba.v2"
    assert new["protocol_sha256"] != old["protocol_sha256"]
    validate_protocol(old)
    validate_protocol(new)
    result = assess_overhead(new, trials(new))
    assert result["passed"]
    assert "common_environment_observer_perturbation_not_measured" in result["limitations"]
    changed = deepcopy(new)
    changed["observer"]["environment_interval_ms"] = 2000
    with pytest.raises(EvidenceError, match="protocol_mismatch"):
        validate_protocol(changed)


def test_projection_requires_known_output_tokens_and_preserves_scope():
    from inferyard.runtime.overhead_runner import request_projection

    row = {
        "case_id": "a",
        "execution_state": "completed",
        "t_send_ns": 0,
        "t_terminal_ns": 100,
        "content": "answer",
        "reasoning": "",
        "completion_tokens": 2,
        "token_source": "engine",
        "token_scope": "content",
    }
    first = request_projection(row)
    assert first["output_identity"]
    assert request_projection({**row, "completion_tokens": None})["output_identity"] is None
    assert (
        request_projection({**row, "completion_tokens": 3})["output_identity"]
        != first["output_identity"]
    )
    assert (
        request_projection({**row, "token_scope": "content+reasoning"})["output_identity"]
        != first["output_identity"]
    )


def test_boundary_protocol_has_distinct_frozen_scope():
    from inferyard.runtime.collector_overhead import validate_protocol

    p = protocol(boundary_observer=True)
    validate_protocol(p)
    assert p["kind"] == "collector_overhead_abba.v3"
    assert p["boundary_observer"]["boundaries"] == ["before_send", "after_terminal"]
    result = assess_overhead(p, trials(p))
    assert result["passed"]
    assert "baseline_environment_is_bracketed_not_continuously_observed" in result["limitations"]
    with pytest.raises(EvidenceError, match="observer_mode"):
        protocol(common_observer=True, boundary_observer=True)
