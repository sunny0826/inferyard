from copy import deepcopy

import pytest

from inferyard.analysis.inter_token_observations import inter_token_observations
from inferyard.analysis.public_metrics import project_metrics
from inferyard.analysis.public_metrics_check import validate_metrics
from inferyard.analysis.throughput_observations import ThroughputInput, throughput_observations
from inferyard.errors import EvidenceError
from inferyard.contracts.validation import json_bytes
from inferyard.reporting.public_package import number

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]


def test_redaction_preserves_distinct_missing_reasons_sources_zero_and_counts():
    row = {
        "metric_id": "C07",
        "definition_version": "phase2.v1",
        "statistic": "observed_max",
        "unit": "celsius",
        "layer": "system",
        "source": "/SECRET/sensor-A",
        "status": "missing",
        "missing_reason": "SECRET-private-reason-A",
        "value": None,
        "sample_count": 0,
        "numerator": None,
        "denominator": 4,
        "excluded": 4,
        "coverage_ratio": None,
        "request_id": "SECRET-request",
        "group": {
            "workload_id": "SECRET-workload",
            "category": "math",
            "error_category": "SECRET-error",
        },
        "limitations": ["SECRET-limitation"],
        "comparison_eligible": False,
        "sampled_start_ns": 100,
        "sampled_end_ns": 130,
        "evidence_refs": [{"path": "/SECRET/evidence"}],
    }
    other = deepcopy(row)
    other.update(source="/SECRET/sensor-B", missing_reason="SECRET-private-reason-B")
    zero = deepcopy(row)
    zero.update(value=0, status="derived", missing_reason=None, excluded=0)
    result = project_metrics([row, other, zero], number)
    assert b"SECRET" not in json_bytes(result)
    a, b, c = result["observations"]
    assert a["source"]["redacted"] and a["source"]["value"] is None
    assert a["source"]["sha256"] != b["source"]["sha256"]
    assert a["missing_reason"]["sha256"] != b["missing_reason"]["sha256"]
    assert a["value"] is None and c["value"] == 0
    assert c["missing_reason"] is None
    assert a["denominator"] == 4 and a["excluded"] == 4
    assert a["sampled_span_ns"] == 30
    assert "sampled_start_ns" not in a and "evidence_refs" not in a
    assert a["request_identity_sha256"] == b["request_identity_sha256"]

    validate_metrics(result)
    mutations = [
        ("observation_count", 2),
        ("value", 0),
        ("denominator", -1),
        ("public_comparison_authorized", True),
        ("status", {"value": "derived", "sha256": "0" * 64, "redacted": False}),
        ("source", {"value": "SECRET-source", "sha256": "0" * 64, "redacted": False}),
    ]
    for key, value in mutations:
        altered = deepcopy(result)
        if key == "observation_count":
            altered[key] = value
        else:
            altered["observations"][0][key] = value
        with pytest.raises(EvidenceError, match="metrics_inconsistent"):
            validate_metrics(altered)


@pytest.mark.parametrize(
    ("statistic", "reason"),
    [
        ("requests_per_second", "no_formal_requests"),
        ("requests_per_second", "terminal_window_unavailable"),
        ("goodput_per_second", "thresholds_not_preregistered"),
        ("goodput_per_second", "goodput_indeterminable"),
        ("goodput_per_second", "quality_not_applicable"),
        ("output_tokens_per_second", "output_token_counts_incomplete"),
        ("output_tokens_per_second", "no_completed_requests"),
        ("token_itl", "no_token_level_event_source"),
        ("service_queue_delay", "no_server_side_queue_events"),
        ("pooled_min", "arrival_capture_missing"),
        ("pooled_min", "not_streaming"),
        ("pooled_min", "insufficient_blocks"),
    ],
)
def test_throughput_vocabulary_is_published_without_redaction(
    statistic: str, reason: str
) -> None:
    limits = [
        "throughput_comparison_gate_pending",
        "serial_trial_no_concurrency",
        "output_token_counts_incomplete",
        "sample_unit_is_block_interval_not_request",
        "arrival_timestamps_batched",
        "not_token_itl",
    ]
    row = {
        "metric_id": "N04",
        "definition_version": "phase2.v1",
        "statistic": statistic,
        "unit": "token/s",
        "layer": "experiment",
        "source": "endpoint.usage/completion_tokens_over_batch_window",
        "status": "missing",
        "missing_reason": reason,
        "value": None,
        "sample_count": 0,
        "numerator": None,
        "denominator": None,
        "excluded": 0,
        "coverage_ratio": None,
        "request_id": None,
        "group": {"workload_id": "workload-1", "category": None, "error_category": None},
        "limitations": limits,
        "comparison_eligible": False,
        "sampled_start_ns": None,
        "sampled_end_ns": None,
        "evidence_refs": [],
    }
    observation = project_metrics([row], number)["observations"][0]
    assert observation["statistic"] == {
        "value": statistic,
        "sha256": observation["statistic"]["sha256"],
        "redacted": False,
    }
    assert observation["source"]["redacted"] is False
    assert observation["missing_reason"]["value"] == reason
    assert all(limit["redacted"] is False for limit in observation["limitations"])


RUN = {
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


def request(index: int, **kwargs: JsonNode) -> JsonObject:
    row = {
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
    row.update(kwargs)
    return row


def reduced_observations() -> list[JsonObject]:
    goodput = throughput_observations(
        ThroughputInput(
            run=RUN,
            workload_id="workload-1",
            evidence=EVIDENCE,
            complete=True,
            thresholds=THRESHOLDS,
        ),
        [
            request(0, t_send_ns=0, t_terminal_ns=None),
            request(1, quality_state="unscorable", t_send_ns=1_000_000_000, t_terminal_ns=1_100_000_000),
        ],
    )
    intervals = inter_token_observations(
        RUN,
        "workload-1",
        [
            request(2),
            request(3, arrival_capture={"streaming": False}),
            request(4, arrival_capture={"streaming": True}, block_arrivals=[{"index": 1, "monotonic_ns": 0}]),
            request(5, execution_state="failed"),
        ],
        EVIDENCE,
        complete=True,
    )
    return goodput + intervals


def test_real_missing_composition_is_published_without_redaction() -> None:
    projected = project_metrics(reduced_observations(), number)
    labels = {
        label["value"]: label["redacted"]
        for row in projected["observations"]
        for label in row["limitations"]
        if label["value"] is not None
    }
    for value in (
        "missing:quality_unscorable:1",
        "missing:latency_missing:1",
        "missing:failed:1",
        "missing:arrival_capture_missing:1",
        "missing:not_streaming:1",
        "missing:insufficient_blocks:1",
    ):
        assert labels[value] is False
    validate_metrics(projected)


def test_unknown_or_malformed_missing_labels_stay_redacted() -> None:
    row = reduced_observations()[0]
    row = deepcopy(row)
    row["limitations"] = [
        "missing:unknown_reason:1",
        "missing:quality_unscorable:x",
        "missing:quality_unscorable:1:2",
    ]
    projected = project_metrics([row], number)
    labels = projected["observations"][0]["limitations"]
    assert all(label["redacted"] is True and label["value"] is None for label in labels)
    validate_metrics(projected)
