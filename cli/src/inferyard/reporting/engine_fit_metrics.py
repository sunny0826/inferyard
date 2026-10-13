"""Descriptive diagnostic observations, with no ranking or qualification claims."""


def _observation(values, operation, missing_reason, *, require_all=False):
    present = [value for value in values if value is not None]
    missing = not present or (require_all and len(present) != len(values))
    return {
        "value": None if missing else operation(present),
        "reason": missing_reason if missing else None,
        "sample_count": len(present),
        "expected_count": len(values),
    }


def _median(values):
    ordered = sorted(values)
    middle = len(ordered) // 2
    if len(ordered) % 2:
        return ordered[middle]
    # Avoid overflowing when both finite durations are very large.
    return ordered[middle - 1] / 2 + ordered[middle] / 2


def observations(data):
    completed = [row["response"] for row in data["requests"] if row["status"] == "completed"]
    resources = data["run"]["resources"]
    return {
        "client_elapsed_median_ms": _observation(
            [row["elapsed_ms"] for row in completed], _median, "no_completed_requests"
        ),
        "client_first_content_median_ms": _observation(
            [row["first_content_ms"] for row in completed], _median, "no_observed_content"
        ),
        "endpoint_completion_tokens_total": _observation(
            [row["completion_tokens"] for row in completed],
            sum,
            "incomplete_endpoint_usage" if completed else "no_completed_requests",
            require_all=True,
        ),
        "boundary_process_tree_rss_max_bytes": _observation(
            [row["process_tree_rss_bytes"] for row in resources],
            max,
            "no_observed_process_tree_rss",
        ),
        "boundary_memory_available_min_bytes": _observation(
            [row["memory_available_bytes"] for row in resources],
            min,
            "no_observed_available_memory",
        ),
    }
