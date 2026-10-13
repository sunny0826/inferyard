"""Allowlisted numeric observations with hashed identities and explicit text redaction."""

import hashlib

from inferyard.evidence.storage import json_bytes
from inferyard.registry import catalogue

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
    "exploratory_p95 not_token_itl repeated_probes_not_independent_cases".split()
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


def fingerprint(value):
    return hashlib.sha256(json_bytes(value)).hexdigest()


def project_metrics(rows, number):
    definitions = catalogue("metrics")["items"]
    vocabulary = VOCABULARY | {
        m[key] for m in definitions for key in ("metric_id", "unit", "layer", "source")
    }

    def label(value):
        if value is None:
            return None
        return {
            "value": value if value in vocabulary else None,
            "sha256": fingerprint(value),
            "redacted": value not in vocabulary,
        }

    result = []
    for index, row in enumerate(rows):
        result.append(
            {
                "observation_index": index,
                **{key: number(row[key]) for key in NUMERIC},
                **{key: label(row[key]) for key in TEXT},
                "request_identity_sha256": fingerprint(row["request_id"])
                if row["request_id"] is not None
                else None,
                "group": {
                    "workload_identity_sha256": fingerprint(row["group"]["workload_id"])
                    if row["group"]["workload_id"] is not None
                    else None,
                    "category": label(row["group"]["category"]),
                    "error_category": label(row["group"]["error_category"]),
                },
                "limitations": [label(value) for value in row["limitations"]],
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
