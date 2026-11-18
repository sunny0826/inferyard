"""N03 placeholders and pooled L05 block intervals stay explicit and non-overlapping."""

import pytest

from inferyard.analysis.inter_token_observations import inter_token_observations
from inferyard.contracts.validation import validate_document

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]

RUN = {
    "origin": "measured",
    "run_id": "run-1",
    "trial_id": "trial-1",
    "definition_versions": {
        "measurement": "phase2.v1",
        "scoring": "phase2.v1",
        "comparison": "phase2.v1",
    },
}
EVIDENCE = [{"path": "events.jsonl", "sha256": "a" * 64}]


def row(index: int, **kwargs: JsonNode) -> JsonObject:
    base = {
        "case_id": f"case-{index}",
        "category": "qa",
        "request_id": f"request-{index}",
        "execution_state": "completed",
        "quality_state": "pass",
        "t_send_ns": index * 1_000_000_000,
        "t_terminal_ns": (index + 1) * 1_000_000_000,
        "completion_tokens": 10,
        "token_source": "endpoint.usage",
        "token_scope": "completion_tokens",
        "arrival_capture": None,
        "block_arrivals": [],
    }
    base.update(kwargs)
    return base


def arrivals(*nanoseconds: int) -> list[dict[str, int]]:
    return [{"index": index + 1, "monotonic_ns": value} for index, value in enumerate(nanoseconds)]


def blocks(
    index: int, *nanoseconds: int, streaming: bool = True, **kwargs: JsonNode
) -> JsonObject:
    return row(
        index,
        arrival_capture={"streaming": streaming},
        block_arrivals=arrivals(*nanoseconds),
        **kwargs,
    )


def reduce(rows: list[JsonObject], *, complete: bool = True) -> list[JsonObject]:
    return inter_token_observations(RUN, "workload-1", rows, EVIDENCE, complete=complete)


def metric(
    items: list[JsonObject], code: str, statistic: str, request: str | None = None
) -> JsonObject:
    matches = [
        item
        for item in items
        if item["metric_id"] == code
        and item["statistic"] == statistic
        and item["request_id"] == request
    ]
    assert len(matches) == 1, (code, statistic, request)
    return matches[0]


def test_n03_has_one_token_itl_placeholder_per_completed_request_and_one_queue_row() -> None:
    items = reduce(
        [
            blocks(0, 0, 1_000_000),
            blocks(1, 0, 1_000_000),
            blocks(2, 0, 1_000_000, execution_state="failed", quality_state="fail"),
        ]
    )
    itl = [item for item in items if item["statistic"] == "token_itl"]
    assert [item["request_id"] for item in itl] == ["request-0", "request-1"]
    for item in itl:
        assert item["metric_id"] == "N03"
        assert item["value"] is None
        assert item["missing_reason"] == "no_token_level_event_source"
        assert item["comparison_eligible"] is False
    queue = metric(items, "N03", "service_queue_delay")
    assert queue["value"] is None
    assert queue["missing_reason"] == "no_server_side_queue_events"
    assert queue["request_id"] is None
    assert queue["group"]["category"] is None
