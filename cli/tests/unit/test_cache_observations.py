from copy import deepcopy

import pytest

from inferyard.analysis.cache_observations import cache_series, observe
from inferyard.analysis.engine_timing import capture_timings


def record(cached=0, processed=0):
    return capture_timings(
        dict(cache_n=cached, prompt_n=processed, predicted_n=2, prompt_ms=0, predicted_ms=1),
        final=True,
    )


@pytest.mark.parametrize(
    "requested,cached,consistent",
    [(False, 0, True), (False, 2, False), (True, 0, True), (True, 2, True)],
)
def test_zero_counts_are_observations_not_missing_rates(requested, cached, consistent):
    row = observe([record(cached)], "completed", True, requested)
    assert row["cached_prompt_tokens"] == cached
    assert row["processed_prompt_tokens"] == 0
    assert row["policy_consistent"] is consistent
    assert row["reuse_observed"] is (cached > 0)
    assert row["missing_reason"] is None


@pytest.mark.parametrize(
    "change",
    ["missing", "ambiguous", "build", "state", "definition", "speculative", "invalid", "bool"],
)
def test_unknown_never_becomes_zero_or_verified(change):
    records = [record(3, 4)]
    build = True
    state = "completed"
    if change == "missing":
        records = []
    elif change == "ambiguous":
        records *= 2
    elif change == "build":
        build = False
    elif change == "state":
        state = "failed"
    elif change == "definition":
        records[0]["definition"] = "other"
    elif change == "speculative":
        records[0]["speculative"] = True
    elif change == "invalid":
        records[0]["missing_reason"] = "invalid_engine_timings"
    else:
        records[0]["cache_n"] = True
    result = observe(records, state, build, True)
    assert result["missing_reason"]
    assert result["cached_prompt_tokens"] is None
    assert result["policy_consistent"] is None


def test_sequence_retains_probes_and_unexecuted_formal_requests():
    events = [
        dict(event_type="request_started", request_id="p", phase="probe", data={"case_id": None}),
        dict(event_type="engine_timings", request_id="p", data=record(0, 10)),
        dict(event_type="request_finished", request_id="p", data={"execution_state": "completed"}),
    ]
    requests = [dict(request_id=None, case_id="c1", execution_state="not_executed")]
    before = deepcopy(events)
    result = cache_series(
        events, {"conditions": {"cache_policy": "enabled"}}, requests, build_verified=True
    )
    assert result["started_requests"] == result["observed_requests"] == 1
    assert result["missing_requests"] == result["planned_formal_requests"] == 1
    assert result["initial_no_reuse_observed"] is True
    assert result["policy_consistent"] is None
    assert result["rows"][0]["requested_reuse"] is False
    assert result["rows"][1]["requested_reuse"] is True
    assert events == before
    events[1]["data"] = record(2, 10)
    assert (
        cache_series(
            events, {"conditions": {"cache_policy": "enabled"}}, requests, build_verified=True
        )["policy_consistent"]
        is False
    )
