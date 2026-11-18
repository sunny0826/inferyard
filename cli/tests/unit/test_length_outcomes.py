from inferyard.analysis.length_outcomes import outcome_cohort


def test_failures_never_become_inferred_rejections_or_capacity_limits():
    rows = [
        {"execution_state": "completed", "error_category": None},
        *[
            {"execution_state": "failed", "error_category": error}
            for error in ("http_error", "total_timeout", "transport_error", None)
        ],
        {"execution_state": "cancelled", "error_category": "temperature_limit"},
        {"execution_state": "invalid", "error_category": "tool_error"},
        {"execution_state": "not_executed", "error_category": None},
    ]
    result = outcome_cohort(rows)
    assert result["terminal_counts"] == {
        "completed": 1,
        "failed": 4,
        "invalid": 1,
        "cancelled": 1,
        "not_executed": 1,
    }
    assert sum(result["terminal_counts"].values()) == len(rows)
    assert result["failure_reasons"] == {
        "http_error": 1,
        "total_timeout": 1,
        "transport_error": 1,
        "unknown": 1,
    }
    assert sum(r["count"] for r in result["excluded_reasons"]) == 3
    assert result["rejected_requests"] is None
    assert result["rejection_missing_reason"] == "explicit_request_rejection_evidence_unavailable"
