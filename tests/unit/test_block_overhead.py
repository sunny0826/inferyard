from copy import deepcopy

import pytest

from inferyard.analysis.block_overhead import DEFINITION, projection
from inferyard.analysis.comparison import compare_trials
from inferyard.evidence.storage import EvidenceError
from inferyard.runtime.collector_overhead import assess_overhead, validate_protocol
from tests.unit.test_collector_overhead import protocol, trials
from tests.unit.test_performance_comparison import inputs


def timeline(gaps=(1,) * 20):
    stamps = [1_000_000]
    for gap in gaps:
        stamps.append(stamps[-1] + int(gap * 1e6))
    return {
        "execution_state": "completed",
        "t_send_ns": 0,
        "t_terminal_ns": stamps[-1] + 1,
        "arrival_capture": {"streaming": True, "source": "decoded_delta"},
        "block_arrivals": [
            {"index": i, "channel": "content", "monotonic_ns": stamp}
            for i, stamp in enumerate(stamps, 1)
        ],
    }


def fixture():
    p = protocol(boundary_observer=True, block_gap_tolerance_ms=1)
    arms = trials(p)
    for i, arm in enumerate(arms):
        for r in arm["requests"]:
            r["block_gaps"] = projection(timeline((1 if i in (0, 3) else 2,) * 20))
    return p, arms


def test_zero_gaps_have_valid_absolute_tolerance_and_p95_requires_twenty():
    r = projection(timeline((0,) * 20))
    assert all(v == 0 for v in r["statistics"].values())
    assert r["gap_count"] == 20
    r = projection(timeline((1,) * 19))
    assert r["statistics"]["within_request_p95"] is None
    assert r["statistics"]["within_request_p50"] == 1
    for bad in (None, False, -1, 0):
        row = timeline()
        row["block_arrivals"][0]["index"] = bad
        assert projection(row)["shape"] is None
    assert projection({**timeline(), "arrival_capture": None})["shape"] is None


def test_per_statistic_gate_does_not_replace_e2e_or_pool_block_samples():
    p, arms = fixture()
    validate_protocol(p)
    result = assess_overhead(p, arms)
    assert result["passed"]
    assert all(r["passed"] for r in result["block_gap_assessments"].values())
    for request in arms[1]["requests"]:
        request["block_gaps"]["statistics"]["within_request_max"] = 5
    result = assess_overhead(p, arms)["block_gap_assessments"]
    assert result["within_request_p50"]["passed"]
    assert not result["within_request_max"]["passed"]
    assert len(result["within_request_max"]["pairs"][0]["case_gap_changes_ms"]) == 2


@pytest.mark.parametrize("change", ["shape", "count", "missing", "nonfinite", "negative"])
def test_unknown_or_changed_chunks_never_qualify(change):
    p, arms = fixture()
    r = arms[2]["requests"][0]["block_gaps"]
    if change == "shape":
        r["shape"] = "b" * 64
    elif change == "count":
        r["gap_count"] += 1
    else:
        r["statistics"]["within_request_p50"] = {
            "missing": None,
            "nonfinite": float("nan"),
            "negative": -1,
        }[change]
    result = assess_overhead(p, arms)["block_gap_assessments"]["within_request_p50"]
    assert not result["passed"] and result["pairs"] == []


def test_absolute_pair_directions_do_not_cancel_and_contract_is_immutable():
    p, arms = fixture()
    for i, gap in ((0, 3), (1, 5), (2, 1), (3, 3)):
        for r in arms[i]["requests"]:
            r["block_gaps"] = projection(timeline((gap,) * 20))
    result = assess_overhead(p, arms)["block_gap_assessments"]["within_request_p50"]
    assert not result["passed"] and result["max_absolute_pair_median_change_ms"] == 2
    q = deepcopy(p)
    q["block_gap_contract"]["tolerance_ms"] = 2
    with pytest.raises(EvidenceError, match="protocol_mismatch"):
        validate_protocol(q)
    for bad in (-1, True, float("inf")):
        with pytest.raises(EvidenceError):
            protocol(boundary_observer=True, block_gap_tolerance_ms=bad)
    with pytest.raises(EvidenceError, match="requires_boundary"):
        protocol(block_gap_tolerance_ms=1)
    assert "block_gap_contract" not in protocol(boundary_observer=True)


def comparison_inputs():
    left, right, proofs = inputs()
    for data, proof in zip((left, right), proofs, strict=True):
        request = data["requests"][0]
        request.update(timeline((0, 2)))
        metric = deepcopy(data["summary"]["metric_observations"][0])
        metric.update(
            metric_id="L05",
            statistic="within_request_p50",
            source="decoded_delta_arrivals",
            request_id=request["request_id"],
            sample_count=2,
            value=0,
        )
        data["summary"]["metric_observations"].append(metric)
        proof["assessment"] = {
            "block_gap_assessments": {
                "within_request_p50": {
                    "passed": True,
                    "definition": DEFINITION,
                    "tolerance_ms": 1,
                }
            }
        }
    return left, right, proofs


@pytest.mark.parametrize(
    "change", [None, "proof", "tolerance", "shape", "cohort", "aggregate", "tokenizer"]
)
def test_only_qualified_per_request_statistics_can_be_compared(change):
    left, right, proofs = comparison_inputs()
    if change == "proof":
        proofs[1].pop("assessment")
    elif change == "tolerance":
        proofs[1]["assessment"]["block_gap_assessments"]["within_request_p50"]["tolerance_ms"] = 2
    elif change == "shape":
        right["requests"][0]["block_arrivals"][0]["channel"] = "reasoning"
    elif change == "cohort":
        right["summary"]["metric_observations"][-1]["sample_count"] = 1
    elif change == "aggregate":
        right["summary"]["metric_observations"][-1]["request_id"] = None
    elif change == "tokenizer":
        right["config"]["model"]["sha256"] = "different"
    before = deepcopy((left, right))
    result = compare_trials(left, right, performance_evidence=proofs)["performance_analysis"]
    rows = [r for r in result["differences"] if r["metric_id"] == "L05"]
    assert all(r["eligible"] is (change is None) for r in rows)
    assert (left, right) == before
