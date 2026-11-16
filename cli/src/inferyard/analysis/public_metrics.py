"""Allowlisted numeric observations with hashed identities and explicit text redaction."""

import hashlib
import re
from collections.abc import Callable, Set

from inferyard.contracts.validation import json_bytes
from inferyard.registry import catalogue

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]
type Number = Callable[[int | float | None], int | float | None]

# Reviewed missing-composition labels only. Unknown reasons keep full redaction.
MISSING_REASONS = frozenset(
    {
        "quality_unscorable",
        "latency_missing",
        "failed",
        "arrival_capture_missing",
        "not_streaming",
        "insufficient_blocks",
    }
)
MISSING_LIMIT = re.compile(r"missing:([a-z_]+):([0-9]+)\Z")

# Only reviewed, fixed vocabulary is published. Unknown strings remain distinguishable
# by hash, but are not disclosed merely because they look like harmless identifiers.
VOCABULARY = frozenset(
    "instruction extraction qa math classification structured performance "
    "observed derived missing not_applicable experiment request engine process system "
    "completion_rate timeout_rate failure_type_rate output_budget_exhaustion_rate "
    "pass_rate whole_case_pass_rate macro_f1 request_value failed_first_event "
    "min p50 p95 max within_request_min within_request_p50 within_request_p95 "
    "within_request_max observed_max observed_min none_observed total_timeout "
    "ratio count seconds ms ns bytes tokens tokens/s percent celsius Hz "
    "phase2.v1 phase2.v2 zero_denominator no_samples insufficient_samples "
    "no_valid_completed_samples request_not_completed sensor_evidence_unavailable "
    "request_not_validly_executed source_changed no_sensor_samples_in_window "
    "performance_task_without_quality_score client_monotonic_events "
    "decoded_delta_arrivals performance_comparison_gate_pending "
    "incomplete_trial_subset_only no_throttling_inference "
    "sampled_extremum_not_instantaneous_peak "
    "coverage_is_sample_span_not_continuous_observation nearest_rank.phase2.v1 "
    "exploratory_p95 not_token_itl repeated_probes_not_independent_cases "
    "requests_per_second goodput_per_second output_tokens_per_second token_itl "
    "service_queue_delay pooled_min pooled_p50 pooled_p95 pooled_max "
    "no_formal_requests terminal_window_unavailable no_completed_requests "
    "output_token_counts_incomplete goodput_indeterminable quality_not_applicable "
    "thresholds_not_preregistered arrival_capture_missing not_streaming "
    "insufficient_blocks latency_missing quality_unscorable "
    "no_token_level_event_source no_server_side_queue_events "
    "throughput_comparison_gate_pending serial_trial_no_concurrency "
    "sample_unit_is_block_interval_not_request arrival_timestamps_batched "
    "endpoint.usage/completion_tokens_over_batch_window".split()
)
NUMERIC = ("value", "sample_count", "numerator", "denominator", "excluded", "coverage_ratio")
TEXT = (
    "metric_id",
    "definition_version",
    "statistic",
    "unit",
    "layer",
    "source",
    "status",
    "missing_reason",
)


def fingerprint(value: JsonNode) -> str:
    return hashlib.sha256(json_bytes(value)).hexdigest()


def is_reviewed(value: str, vocabulary: Set[str]) -> bool:
    """Accept exact vocabulary or reviewed reason:count missing compositions."""
    if value in vocabulary:
        return True
    match = MISSING_LIMIT.fullmatch(value)
    return bool(match and match[1] in MISSING_REASONS)


def _vocabulary() -> Set[str]:
    definitions = catalogue("metrics")["items"]
    return VOCABULARY | {
        metric[key] for metric in definitions for key in ("metric_id", "unit", "layer", "source")
    }


def _public_label(value: str | None, vocabulary: Set[str]) -> dict[str, str | bool | None] | None:
    if value is None:
        return None
    reviewed = is_reviewed(value, vocabulary)
    return {
        "value": value if reviewed else None,
        "sha256": fingerprint(value),
        "redacted": not reviewed,
    }


def project_metrics(rows: list[JsonObject], number: Number) -> JsonObject:
    vocabulary = _vocabulary()
    result = []
    for index, row in enumerate(rows):
        result.append(
            {
                "observation_index": index,
                **{key: number(row[key]) for key in NUMERIC},
                **{key: _public_label(row[key], vocabulary) for key in TEXT},
                "request_identity_sha256": fingerprint(row["request_id"])
                if row["request_id"] is not None
                else None,
                "group": {
                    "workload_identity_sha256": fingerprint(row["group"]["workload_id"])
                    if row["group"]["workload_id"] is not None
                    else None,
                    "category": _public_label(row["group"]["category"], vocabulary),
                    "error_category": _public_label(row["group"]["error_category"], vocabulary),
                },
                "limitations": [
                    _public_label(value, vocabulary) for value in row["limitations"]
                ],
                "source_comparison_eligible": row["comparison_eligible"],
                "public_comparison_authorized": False,
                "sampled_span_ns": number(row["sampled_end_ns"] - row["sampled_start_ns"])
                if row["sampled_start_ns"] is not None and row["sampled_end_ns"] is not None
                else None,
            }
        )
    return {
        "projection_version": "public-metrics.v1",
        "observation_count": len(result),
        "observations": result,
        "limitations": [
            "labels_outside_reviewed_vocabulary_are_redacted_with_hash",
            "hashes_are_linkable_not_anonymity_guarantees",
            "absolute_clock_values_and_raw_evidence_references_omitted",
            "source_eligibility_is_not_public_pair_comparison_authorization",
        ],
    }
