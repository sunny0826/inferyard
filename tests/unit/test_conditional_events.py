import pytest

from inferyard.evidence.conditional_events import queue_delay, startup, token_intervals
from inferyard.evidence.storage import EvidenceError


def test_content_chunks_and_missing_lifecycle_never_substitute_true_events():
    assert token_intervals({"source_semantics": "content_chunks"}, "c")["value"] is None
    assert queue_delay({}, "c")["value"] is None
    assert startup({}, "c")["first_valid_answer_seconds"] is None


def test_true_token_indices_usage_and_clock_recompute_median_interval():
    row = {
        "source_semantics": "verified_per_token_events",
        "usage_tokens": 3,
        "events": [
            {"clock_id": "c", "token_index": i, "token_id": 20 + i, "monotonic_ns": t}
            for i, t in enumerate([0, 2_000_000, 6_000_000])
        ],
    }
    assert token_intervals(row, "c")["value"] == 3
    row["events"][1]["clock_id"] = "other"
    with pytest.raises(EvidenceError, match="clock"):
        token_intervals(row, "c")


def test_queue_pair_keeps_exact_request_and_server_clock():
    row = {
        "source_semantics": "verified_server_queue_events",
        "events": [
            {"request_id": "r", "event": "enqueued", "clock_id": "c", "monotonic_ns": 1},
            {
                "request_id": "r",
                "event": "execution_started",
                "clock_id": "c",
                "monotonic_ns": 2_000_001,
            },
        ],
    }
    assert queue_delay(row, "c")["value"] == 2
    row["events"][1]["request_id"] = "different"
    with pytest.raises(EvidenceError, match="identity"):
        queue_delay(row, "c")


def test_process_start_ready_first_answer_are_distinct_from_check_time():
    row = {
        "source_semantics": "verified_process_lifecycle_events",
        "start_kind": "cold_process",
        "os_cache_state": "unknown",
        "events": [
            {
                "event": e,
                "pid": 100,
                "process_start_ticks": 50,
                "clock_id": "c",
                "monotonic_ns": t,
                "protocol_complete": True,
            }
            for e, t in zip(
                ["process_started", "ready", "first_valid_answer"],
                [0, 1_000_000_000, 3_000_000_000],
                strict=True,
            )
        ],
    }
    result = startup(row, "c")
    assert result["ready_seconds"] == 1 and result["first_valid_answer_seconds"] == 3
    assert result["os_cache_state"] == "unknown"
    row["events"][-1]["pid"] = 101
    with pytest.raises(EvidenceError, match="process_changed"):
        startup(row, "c")
