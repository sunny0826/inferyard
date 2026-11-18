"""Hand-calculated client timelines, privacy buffering and missing evidence."""

import json

import pytest

from inferyard.adapters.prism import ResponseState, StreamProtocol
from inferyard.analysis.performance import distribution, request_timing, summarize_performance
from inferyard.contracts.redaction import Redactor
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


def test_missing_evidence_empty_answer_and_ordinary_response():
    row, _ = timeline(streaming=False)
    assert request_timing(row)["L05"]["reason"] == "not_streaming"
    row.pop("arrival_capture")
    row.update(t_first_answer_ns=None, completion_tokens=None)
    result = request_timing(row)
    assert result["L02"]["reason"] == "no_answer_event"
    assert result["L04"]["value"] is None
    assert result["L05"]["reason"] == "evidence_missing"
