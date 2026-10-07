"""Length-cohort terminal accounting, without inferring context rejection from failure."""

from collections import Counter

STATES = ("completed", "failed", "invalid", "cancelled", "not_executed")


def outcome_cohort(requests):
    states = Counter(row["execution_state"] for row in requests)
    failures = Counter(
        row.get("error_category") or "unknown"
        for row in requests
        if row["execution_state"] == "failed"
    )
    excluded = Counter(
        (row["execution_state"], row.get("error_category") or "unknown")
        for row in requests
        if row["execution_state"] in ("invalid", "cancelled", "not_executed")
    )
    valid = [r for r in requests if r["execution_state"] in ("completed", "failed")]
    statuses = Counter(
        r["http_response"]["status_code"] for r in valid if r.get("http_response") is not None
    )
    unknown = len(valid) - sum(statuses.values())
    rejected = sum(count for status, count in statuses.items() if 400 <= status <= 499)
    return {
        "terminal_counts": {state: states[state] for state in STATES},
        "failure_reasons": dict(sorted(failures.items())),
        "excluded_reasons": [
            {"state": state, "reason": reason, "count": count}
            for (state, reason), count in sorted(excluded.items())
        ],
        "http_status_counts": {str(k): v for k, v in sorted(statuses.items())},
        "http_status_unknown_requests": unknown,
        "observed_http_4xx_requests": rejected,
        "rejected_requests": rejected if valid and not unknown else None,
        "rejection_missing_reason": None
        if valid and not unknown
        else "explicit_request_rejection_evidence_unavailable",
        "rejection_definition": "http_4xx_response_not_context_capacity_rejection",
        "limitations": [
            "http_error_timeout_and_transport_failure_do_not_prove_context_rejection",
            "cancelled_invalid_and_unexecuted_do_not_prove_model_capacity_limit",
            "completed_request_does_not_prove_long_context_comprehension",
        ],
    }
