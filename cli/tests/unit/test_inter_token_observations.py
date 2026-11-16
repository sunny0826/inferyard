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


def test_pooled_l05_keeps_zero_intervals_and_labels_the_batched_arrival_limit() -> None:
    items = reduce([blocks(0, 0, 400_000_000, 400_000_000)])
    assert metric(items, "L05", "pooled_min")["value"] == 0
    assert metric(items, "L05", "pooled_max")["value"] == 400
    pooled = metric(items, "L05", "pooled_min")
    assert pooled["sample_count"] == 2
    assert "sample_unit_is_block_interval_not_request" in pooled["limitations"]
    assert "not_token_itl" in pooled["limitations"]
    assert "arrival_timestamps_batched" in pooled["limitations"]


def test_pooled_l05_never_reuses_within_request_statistic_names() -> None:
    items = reduce([blocks(0, 0, 1_000_000)])
    statistics = {item["statistic"] for item in items if item["metric_id"] == "L05"}
    assert statistics == {"pooled_min", "pooled_p50", "pooled_p95", "pooled_max"}


def test_failed_requests_are_neither_candidates_nor_pooled_and_are_counted() -> None:
    items = reduce(
        [
            blocks(0, 0, 1_000_000),
            blocks(1, 0, 2_000_000, execution_state="failed", quality_state="fail"),
        ]
    )
    pooled = metric(items, "L05", "pooled_min")
    assert pooled["sample_count"] == 1
    assert pooled["value"] == 1
    assert "missing:failed:1" in pooled["limitations"]


@pytest.mark.parametrize(
    "count,expected_p95,exploratory",
    [(20, 1, True), (19, None, False), (21, 1, True)],
)
def test_pooled_l05_p95_follows_the_shared_twenty_sample_rule(
    count: int, expected_p95: float | None, exploratory: bool
) -> None:
    items = reduce([blocks(0, *[index * 1_000_000 for index in range(count + 1)])])
    p95 = metric(items, "L05", "pooled_p95")
    assert p95["sample_count"] == count
    assert p95["value"] == expected_p95
    assert ("exploratory_p95" in p95["limitations"]) is exploratory
    if expected_p95 is None:
        assert p95["missing_reason"] == "insufficient_samples"


@pytest.mark.parametrize(
    "requests,reason",
    [
        ([row(0, execution_state="failed")], "no_completed_requests"),
        ([row(0), row(1)], "arrival_capture_missing"),
        ([blocks(0, streaming=False)], "not_streaming"),
        ([blocks(0, 0)], "insufficient_blocks"),
        ([row(0), blocks(1, streaming=False)], "not_streaming"),
    ],
)
def test_pooled_l05_zero_sample_reason_priority_is_frozen(
    requests: list[JsonObject], reason: str
) -> None:
    pooled = metric(reduce(requests), "L05", "pooled_min")
    assert pooled["value"] is None
    assert pooled["missing_reason"] == reason


def test_pooled_l05_streaming_capture_with_zero_blocks_is_insufficient_blocks() -> None:
    pooled = metric(reduce([blocks(0)]), "L05", "pooled_min")
    assert pooled["value"] is None
    assert pooled["missing_reason"] == "insufficient_blocks"


def test_pooled_l05_mixed_capture_states_follow_the_frozen_priority() -> None:
    rows = [row(0), blocks(1, streaming=False), blocks(2, 0)]
    pooled = metric(reduce(rows), "L05", "pooled_min")
    assert pooled["value"] is None
    assert pooled["missing_reason"] == "insufficient_blocks"


def test_pooled_l05_mixed_missing_composition_is_counted_in_limits() -> None:
    items = reduce([row(0), blocks(1, 0, 1_000_000)])
    pooled = metric(items, "L05", "pooled_min")
    assert pooled["value"] == 1
    assert "missing:arrival_capture_missing:1" in pooled["limitations"]


def test_pooled_failed_count_excludes_cancelled_invalid_and_not_executed() -> None:
    rows = [
        blocks(0, 0, 1_000_000),
        row(1, execution_state="failed"),
        row(2, execution_state="cancelled"),
        row(3, execution_state="invalid"),
        row(4, execution_state="not_executed"),
    ]
    limits = metric(reduce(rows), "L05", "pooled_min")["limitations"]
    assert "missing:failed:1" in limits
    assert not any(value.startswith("missing:failed:") and value != "missing:failed:1" for value in limits)


def test_inter_token_observations_are_valid_contract_observations() -> None:
    for item in reduce([blocks(0, 0, 1_000_000)]):
        validate_document("metric_observation", item)
