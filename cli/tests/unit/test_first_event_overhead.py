from copy import deepcopy

import pytest

from inferyard.analysis.first_event_overhead import projection
from inferyard.evidence.storage import EvidenceError
from inferyard.runtime.collector_overhead import assess_overhead, validate_protocol
from tests.unit.test_collector_overhead import protocol, trials


def fixture():
    p = protocol(boundary_observer=True, first_event_tolerance_ratio=0.05)
    t = trials(p)
    for i, arm in enumerate(t):
        for r in arm["requests"]:
            r["first_event_ns"] = {
                "L01": 100 if i in (0, 3) else 105,
                "L02": 200 if i in (0, 3) else 208,
            }
    return p, t


def test_first_event_contract_is_explicit_and_each_metric_is_separate():
    p, t = fixture()
    validate_protocol(p)
    result = assess_overhead(p, t)
    assert all(v["passed"] for v in result["first_event_assessments"].values())
    for r in t[1]["requests"]:
        r["first_event_ns"]["L01"] = 106
    result = assess_overhead(p, t)
    assert result["passed"]  # E2E qualification does not imply first-event qualification.
    assert not result["first_event_assessments"]["L01"]["passed"]
    assert result["first_event_assessments"]["L02"]["passed"]


@pytest.mark.parametrize("value", [None, 0, -1, True, float("nan")])
def test_missing_or_zero_first_events_never_qualify(value):
    p, t = fixture()
    t[2]["requests"][0]["first_event_ns"]["L02"] = value
    result = assess_overhead(p, t)["first_event_assessments"]["L02"]
    assert not result["passed"] and result["pairs"] == []


def test_opposite_signs_do_not_cancel():
    p, t = fixture()
    for i, value in ((1, 110), (2, 90)):
        for r in t[i]["requests"]:
            r["first_event_ns"]["L01"] = value
    a = assess_overhead(p, t)["first_event_assessments"]["L01"]
    assert not a["passed"] and a["max_absolute_pair_median_change"] == 0.1


def test_frozen_tolerance_and_baseline_required():
    for value in (-1, 1, True, float("inf")):
        with pytest.raises(EvidenceError):
            protocol(boundary_observer=True, first_event_tolerance_ratio=value)
    with pytest.raises(EvidenceError, match="requires_boundary"):
        protocol(first_event_tolerance_ratio=0.05)
    p, _ = fixture()
    q = deepcopy(p)
    q["first_event_contract"]["tolerance_ratio"] = 0.1
    with pytest.raises(EvidenceError, match="protocol_mismatch"):
        validate_protocol(q)
    assert "first_event_contract" not in protocol(boundary_observer=True)


def test_first_event_projection_uses_raw_clocks_and_rejects_bad_bounds():
    row = {
        "t_send_ns": 100,
        "t_terminal_ns": 1000,
        "t_first_content_ns": 150,
        "t_first_answer_ns": 300,
    }
    assert projection(row) == {"L01": 50, "L02": 200}
    for value in (None, 100, 99, 1001, True):
        assert projection({**row, "t_first_content_ns": value})["L01"] is None
