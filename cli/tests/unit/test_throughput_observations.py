"""N01/N02/N04 reductions: frozen windows, measured zero and goodput truth table."""

import pytest

from inferyard.analysis.throughput_observations import ThroughputInput, throughput_observations
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


def test_n01_counts_completed_requests_over_the_send_to_last_terminal_window() -> None:
    items = reduce([row(0), row(1), row(2)])
    item = metric(items, "N01", "requests_per_second")
    assert item["value"] == pytest.approx(1.0)
    assert item["unit"] == "request/s"
    assert item["sample_count"] == 3
    assert item["numerator"] is None and item["denominator"] is None
    assert item["group"] == {"workload_id": "workload-1", "category": None, "error_category": None}
    assert item["comparison_eligible"] is False
    assert "throughput_comparison_gate_pending" in item["limitations"]
    assert "serial_trial_no_concurrency" in item["limitations"]


def test_n01_zero_numerator_is_a_measured_zero_not_a_failure() -> None:
    items = reduce([row(0, execution_state="failed", quality_state="fail")])
    item = metric(items, "N01", "requests_per_second")
    assert item["value"] == 0
    assert item["missing_reason"] is None
    assert item["status"] == "derived"


@pytest.mark.parametrize(
    "case,reason",
    [
        (row(0, t_send_ns=None, t_terminal_ns=None), "no_formal_requests"),
        (row(0, t_terminal_ns=None), "terminal_window_unavailable"),
        (row(0, t_send_ns=5, t_terminal_ns=5), "terminal_window_unavailable"),
    ],
)
def test_n01_missing_window_keeps_an_explicit_reason(case: JsonObject, reason: str) -> None:
    item = metric(reduce([case]), "N01", "requests_per_second")
    assert item["value"] is None
    assert item["missing_reason"] == reason


def test_n04_sums_endpoint_completion_tokens_over_the_same_window() -> None:
    items = reduce(
        [
            row(0, completion_tokens=10),
            row(1, completion_tokens=20),
            row(2, completion_tokens=30),
        ]
    )
    item = metric(items, "N04", "output_tokens_per_second")
    assert item["value"] == pytest.approx(20.0)
    assert item["unit"] == "token/s"
    assert item["sample_count"] == 3
    assert item["source"] == "endpoint.usage/completion_tokens_over_batch_window"


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


def test_n04_all_zero_tokens_is_a_measured_zero() -> None:
    item = metric(
        reduce([row(0, completion_tokens=0), row(1, completion_tokens=0)]),
        "N04",
        "output_tokens_per_second",
    )
    assert item["value"] == 0
    assert item["missing_reason"] is None


def test_n04_without_completed_requests_is_not_a_measured_zero() -> None:
    item = metric(
        reduce([row(0, execution_state="failed", quality_state="fail")]),
        "N04",
        "output_tokens_per_second",
    )
    assert item["value"] is None
    assert item["missing_reason"] == "no_completed_requests"


def test_n02_accepted_requests_are_quality_and_latency_conjunction() -> None:
    rows = [
        row(0, t_send_ns=0, t_terminal_ns=100_000_000),
        row(1, t_send_ns=1_000_000_000, t_terminal_ns=1_100_000_000),
    ]
    item = metric(reduce(rows), "N02", "goodput_per_second")
    assert item["value"] == pytest.approx(2 / 1.1)
    assert item["sample_count"] == 2
    assert item["unit"] == "request/s"


def test_n02_all_completed_quality_failures_are_a_measured_zero() -> None:
    item = metric(
        reduce([row(0, quality_state="fail"), row(1, quality_state="fail")]),
        "N02",
        "goodput_per_second",
    )
    assert item["value"] == 0
    assert item["missing_reason"] is None


def test_n02_mixed_accepted_and_failed_requests_keep_the_measured_rate() -> None:
    rows = [
        row(0, t_send_ns=0, t_terminal_ns=100_000_000),
        row(1, quality_state="fail", t_send_ns=1_000_000_000, t_terminal_ns=1_100_000_000),
    ]
    item = metric(reduce(rows), "N02", "goodput_per_second")
    assert item["value"] == pytest.approx(1 / 1.1)


def test_n02_unscorable_request_over_the_latency_threshold_is_not_met() -> None:
    item = metric(
        reduce([row(0, quality_state="unscorable", t_send_ns=0, t_terminal_ns=1_000_000_000)]),
        "N02",
        "goodput_per_second",
    )
    assert item["value"] == 0
    assert item["missing_reason"] is None


def test_n02_unscorable_request_within_the_threshold_is_indeterminate() -> None:
    item = metric(
        reduce([row(0, quality_state="unscorable", t_send_ns=0, t_terminal_ns=400_000_000)]),
        "N02",
        "goodput_per_second",
    )
    assert item["value"] is None
    assert item["missing_reason"] == "goodput_indeterminable"
    assert "missing:quality_unscorable:1" in item["limitations"]


def test_n04_all_missing_token_counts_are_null_with_every_exclusion() -> None:
    rows = [
        row(0, completion_tokens=None),
        row(1, completion_tokens=None),
        row(2, completion_tokens=None),
    ]
    item = metric(reduce(rows), "N04", "output_tokens_per_second")
    assert item["value"] is None
    assert item["missing_reason"] == "output_token_counts_incomplete"
    assert item["excluded"] == 3
    assert item["sample_count"] == 0


def test_n02_completed_quality_failure_without_latency_is_not_met() -> None:
    rows = [
        row(0, t_send_ns=0, t_terminal_ns=100_000_000),
        row(1, execution_state="completed", quality_state="fail", t_send_ns=200_000_000, t_terminal_ns=None),
    ]
    item = metric(reduce(rows), "N02", "goodput_per_second")
    assert item["value"] == pytest.approx(1 / 0.1)
    assert item["sample_count"] == 2


def test_n02_passing_quality_without_latency_is_indeterminate() -> None:
    rows = [
        row(0, t_send_ns=0, t_terminal_ns=100_000_000),
        row(1, t_send_ns=200_000_000, t_terminal_ns=None),
    ]
    item = metric(reduce(rows), "N02", "goodput_per_second")
    assert item["value"] is None
    assert item["missing_reason"] == "goodput_indeterminable"
    assert "missing:latency_missing:1" in item["limitations"]


def test_n02_quality_not_applicable_is_not_a_quality_failure() -> None:
    item = metric(
        reduce([row(0, quality_state="not_applicable")]), "N02", "goodput_per_second"
    )
    assert item["value"] is None
    assert item["missing_reason"] == "quality_not_applicable"


def test_n02_without_preregistered_thresholds_is_explicitly_missing() -> None:
    item = metric(reduce([row(0)], thresholds=None), "N02", "goodput_per_second")
    assert item["value"] is None
    assert item["missing_reason"] == "thresholds_not_preregistered"


def test_n02_missing_window_precedes_threshold_configuration() -> None:
    item = metric(
        reduce([row(0, t_send_ns=None, t_terminal_ns=None)], thresholds=None),
        "N02",
        "goodput_per_second",
    )
    assert item["missing_reason"] == "no_formal_requests"


def test_every_throughput_observation_is_a_valid_contract_observation() -> None:
    for item in reduce([row(0), row(1, quality_state="unscorable")]):
        validate_document("metric_observation", item)
