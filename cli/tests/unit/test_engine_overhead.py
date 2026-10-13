from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.engine_overhead import CONTRACT, projection
from inferyard.analysis.engine_timing import DEFINITION
from inferyard.evidence.storage import EvidenceError
from inferyard.runtime.collector_overhead import assess_overhead, validate_protocol
from tests.unit.test_collector_overhead import protocol, trials
from tests.unit.test_engine_timing import row
from tests.unit.test_performance_comparison import inputs


def fixture():
    p = protocol(boundary_observer=True, engine_rate_tolerance_ratio=0.05)
    arms = trials(p)
    for i, arm in enumerate(arms):
        for request in arm["requests"]:
            request["engine_rates"] = projection(row())
            if i in (1, 2):
                request["engine_rates"]["L06"]["value"] *= 1.04
                request["engine_rates"]["L07"]["value"] *= 1.03
    return p, arms


def test_each_engine_metric_qualifies_independently_of_e2e():
    p, arms = fixture()
    validate_protocol(p)
    assert all(r["passed"] for r in assess_overhead(p, arms)["engine_rate_assessments"].values())
    for request in arms[1]["requests"]:
        request["engine_rates"]["L07"]["value"] *= 1.2
    result = assess_overhead(p, arms)
    assert result["passed"] and result["engine_rate_assessments"]["L06"]["passed"]
    assert not result["engine_rate_assessments"]["L07"]["passed"]


@pytest.mark.parametrize("change", ["missing", "zero", "cache", "work", "source", "reason"])
def test_rate_requires_verified_equal_processed_work(change):
    p, arms = fixture()
    rate = arms[2]["requests"][0]["engine_rates"]["L06"]
    if change == "missing":
        rate.clear()
    elif change == "zero":
        rate["value"] = 0
    elif change == "cache":
        rate["cache_tokens"] += 1
    elif change == "work":
        rate["processed_tokens"] += 1
    elif change == "source":
        rate["source"] = "different"
    else:
        rate["reason"] = "unknown"
    result = assess_overhead(p, arms)["engine_rate_assessments"]["L06"]
    assert not result["passed"] and not result["pairs"]


def test_frozen_contract_and_absolute_pair_changes():
    p, arms = fixture()
    for i, value in ((1, 220), (2, 180)):
        for request in arms[i]["requests"]:
            request["engine_rates"]["L06"]["value"] = value
    result = assess_overhead(p, arms)["engine_rate_assessments"]["L06"]
    assert not result["passed"] and result["max_absolute_pair_median_change"] == 0.1
    changed = deepcopy(p)
    changed["engine_rate_contract"]["tolerance_ratio"] = 0.5
    with pytest.raises(EvidenceError, match="protocol_mismatch"):
        validate_protocol(changed)
    with pytest.raises(EvidenceError, match="requires_boundary"):
        protocol(engine_rate_tolerance_ratio=0.05)
    for bad in (True, -1, 1, float("nan")):
        with pytest.raises(EvidenceError):
            protocol(boundary_observer=True, engine_rate_tolerance_ratio=bad)
    assert "engine_rate_contract" not in protocol(boundary_observer=True)


def comparison_inputs():
    left, right, proofs = inputs()
    for data, proof in zip((left, right), proofs, strict=True):
        data["requests"][0].update(row())
        metric = deepcopy(data["summary"]["metric_observations"][2])
        metric.update(
            metric_id="L06", source="verified_engine_timings", layer="engine", unit="token/s"
        )
        data["summary"]["metric_observations"].append(metric)
        proof["assessment"] = {
            "engine_rate_assessments": {
                "L06": {
                    "passed": True,
                    "definition": CONTRACT,
                    "timing_definition": DEFINITION,
                    "tolerance_ratio": 0.05,
                }
            }
        }
    return left, right, proofs


@pytest.mark.parametrize(
    "change", [None, "proof", "tolerance", "cache", "count", "build", "tokenizer", "source"]
)
def test_context_requires_engine_proof_and_matching_work(change):
    left, right, proofs = comparison_inputs()
    if change == "proof":
        proofs[1].pop("assessment")
    elif change == "tolerance":
        proofs[1]["assessment"]["engine_rate_assessments"]["L06"]["tolerance_ratio"] = 0.1
    elif change in ("cache", "count"):
        right["requests"][0]["engine_timings"][0][
            "cache_n" if change == "cache" else "prompt_n"
        ] += 1
    elif change == "build":
        right["requests"][0]["engine_build_verified"] = False
    elif change == "tokenizer":
        right["config"]["model"]["sha256"] = "other"
    elif change == "source":
        right["summary"]["metric_observations"][-1]["source"] = "unknown"
    original = deepcopy((left, right))
    result = compare_trials(left, right, performance_evidence=proofs)["performance_analysis"]
    rows = [r for r in result["differences"] if r["metric_id"] == "L06"]
    assert all(r["eligible"] is (change is None) for r in rows)
    assert (left, right) == original
