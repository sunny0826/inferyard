"""N04 throughput reduction must stay null when token counts are partial or untrusted."""

import pytest

from inferyard.analysis.throughput_observations import ThroughputInput, throughput_observations

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
THRESHOLDS = {"quality_state": "pass", "e2e_latency_ms_max": 500}


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


def reduce(
    rows: list[JsonObject],
    *,
    thresholds: JsonObject | None = THRESHOLDS,
    complete: bool = True,
) -> list[JsonObject]:
    return throughput_observations(
        ThroughputInput(
            run=RUN,
            workload_id="workload-1",
            evidence=EVIDENCE,
            complete=complete,
            thresholds=thresholds,
        ),
        rows,
    )


def metric(items: list[JsonObject], code: str, statistic: str) -> JsonObject:
    matches = [item for item in items if item["metric_id"] == code and item["statistic"] == statistic]
    assert len(matches) == 1, (code, statistic)
    return matches[0]


@pytest.mark.parametrize("missing", [{"completion_tokens": None}, {"token_source": "client"}])
def test_n04_partial_token_counts_make_the_total_null_and_count_exclusions(
    missing: JsonObject,
) -> None:
    items = reduce([row(0), row(1, **missing), row(2)])
    item = metric(items, "N04", "output_tokens_per_second")
    assert item["value"] is None
    assert item["missing_reason"] == "output_token_counts_incomplete"
    assert item["excluded"] == 1
    assert item["sample_count"] == 2
    assert "output_token_counts_incomplete" in item["limitations"]
