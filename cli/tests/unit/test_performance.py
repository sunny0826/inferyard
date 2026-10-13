"""Hand-calculated client timelines, privacy buffering and missing evidence."""

import json

import pytest

from inferyard.adapters.prism import ResponseState, StreamProtocol
from inferyard.analysis.performance import distribution, request_timing, summarize_performance
from inferyard.evidence.storage import Redactor
from tests.unit.test_prism import frame


def timeline(secret=None, streaming=True):
    events = []
    state = ResponseState(0)
    protocol = StreamProtocol(
        state,
        Redactor([secret] if secret else []),
        lambda *args: events.append(args),
        arrivals=True,
        streaming=streaming,
    )
    protocol.accept(frame({"content": ""}), 10_000_000)
    protocol.accept(frame({"reasoning_content": "想"}), 100_000_000)
    for text, stamp in [("private-", 200_000_000), ("credential", 500_000_000)]:
        protocol.accept(frame({"content": text}), stamp)
    protocol.accept(frame(reason="stop", usage={"completion_tokens": 12}), 1_000_000_000)
    protocol.accept("[DONE]", 1_200_000_000)
    row = {
        **state.terminal(1_200_000_000),
        "category": "qa",
        "request_id": "a",
        "arrival_capture": events[0][1],
        "block_arrivals": [
            {**data, "monotonic_ns": t} for kind, data, t in events if kind == "block_arrived"
        ],
    }
    return row, events


def test_arrival_timeline_survives_redaction_without_recording_secret():
    plain, _ = timeline()
    redacted, events = timeline("private-credential")
    assert "private-credential" not in json.dumps(events)
    assert redacted["content"] == "[REDACTED]"
    assert redacted["block_arrivals"] == plain["block_arrivals"]
    measured = request_timing(redacted)
    assert measured["L01"]["value"] == 100
    assert measured["L02"]["value"] == 200
    assert measured["L03"]["value"] == 1200
    assert measured["L04"]["value"] == 10
    assert measured["L05"]["intervals"] == [100, 300]
    assert measured["L05"]["distribution"]["p50"] == 100
    assert all(set(a) == {"index", "channel", "monotonic_ns"} for a in redacted["block_arrivals"])


@pytest.mark.parametrize("count,p95", [(0, None), (19, None), (20, 19), (100, 95)])
def test_nearest_rank_and_small_sample_policy(count, p95):
    result = distribution(list(range(1, count + 1)))
    assert result["p95"] == p95
    assert result["p95_exploratory"] == (20 <= count < 100)
    if count == 20:
        assert result["p50"] == 10


def test_failure_excluded_and_categories_not_pooled():
    row, _ = timeline()
    failure = {
        **row,
        "request_id": "b",
        "execution_state": "failed",
        "error_category": "total_timeout",
        "t_terminal_ns": 5_000_000_000,
    }
    other = {**row, "request_id": "c", "category": "math", "t_terminal_ns": 2_000_000_000}
    groups = summarize_performance([row, failure, other])
    assert groups["qa"]["metrics"]["L03"]["max"] == 1200
    assert groups["qa"]["metrics"]["L03"]["excluded"] == 1
    assert groups["qa"]["timeout_count"] == 1
    assert groups["qa"]["failed_first_events"][0]["L01"]["value"] == 100
    assert groups["math"]["metrics"]["L03"]["max"] == 2000
    assert len(groups["qa"]["metrics"]["L05"]) == 1


def test_missing_evidence_empty_answer_and_ordinary_response():
    row, _ = timeline(streaming=False)
    assert request_timing(row)["L05"]["reason"] == "not_streaming"
    row.pop("arrival_capture")
    row.update(t_first_answer_ns=None, completion_tokens=None)
    result = request_timing(row)
    assert result["L02"]["reason"] == "no_answer_event"
    assert result["L04"]["value"] is None
    assert result["L05"]["reason"] == "evidence_missing"


def test_same_read_blocks_preserve_zero_interval():
    row, _ = timeline()
    row["block_arrivals"][1]["monotonic_ns"] = 100_000_000
    assert request_timing(row)["L05"]["intervals"] == [0, 400]
