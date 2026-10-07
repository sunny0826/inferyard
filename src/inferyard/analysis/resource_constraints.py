"""Explicit all-request resource bounds, retaining missing and failed request slots."""

import math


def finite(value):
    try:
        return type(value) in (int, float) and math.isfinite(value)
    except OverflowError:
        return False


def evaluate(data, comparison, constraint, eligible):
    category = constraint["category"]
    requests = [
        r for r in data.get("requests", []) if category is None or r["category"] == category
    ]
    complete = data["summary"]["completeness"] == "complete"
    identities = [(r.get("request_id"), r["case_id"]) for r in requests]
    unique = len({r[0] for r in identities}) == len(identities) and len(
        {r[1] for r in identities}
    ) == len(identities)
    outcomes = []
    for request in requests:
        matches = [
            m
            for m in data["summary"].get("metric_observations", [])
            if request.get("request_id") is not None
            and m.get("request_id") == request["request_id"]
            and m["group"].get("category") == request["category"]
            and m["group"].get("error_category") is None
            and all(
                m.get(k) == constraint[k]
                for k in ("metric_id", "statistic", "source", "unit", "definition_version")
            )
        ]
        metric = matches[0] if len(matches) == 1 else None
        value = metric.get("value") if metric else None
        reason = None
        if not complete:
            reason = "incomplete_trial"
        elif not unique:
            reason = "duplicate_request_or_case_scope"
        elif request["execution_state"] != "completed":
            reason = "request_not_completed"
        elif metric is None:
            reason = "metric_missing" if not matches else "ambiguous_metric"
        elif not finite(value) or metric.get("missing_reason") is not None:
            reason = "metric_value_unknown"
        elif not eligible(metric, comparison, case_id=request["case_id"]):
            reason = "metric_not_comparable"
        passed = reason is None and (
            value <= constraint["threshold"]
            if constraint["operator"] == "<="
            else value >= constraint["threshold"]
        )
        outcomes.append(
            {
                "request_id": request.get("request_id"),
                "case_id": request["case_id"],
                "category": request["category"],
                "execution_state": request["execution_state"],
                "value": value if finite(value) else None,
                "status": "unknown" if reason else "pass" if passed else "fail",
                "reason": reason,
            }
        )
    counts = {
        status: sum(r["status"] == status for r in outcomes)
        for status in ("pass", "fail", "unknown")
    }
    known = bool(outcomes) and counts["unknown"] == 0
    operation = max if constraint["operator"] == "<=" else min
    value = operation(r["value"] for r in outcomes) if known else None
    status = "fail" if counts["fail"] else "unknown" if not known else "pass"
    return {
        "constraint": constraint,
        "value": value,
        "status": status,
        "reason": "no_planned_requests_in_scope"
        if not outcomes
        else "one_or_more_requests_unknown"
        if counts["unknown"]
        else None,
        "aggregation": {
            "definition": "resource_all_requests.v1",
            "operation": "max_of_request_observations"
            if constraint["operator"] == "<="
            else "min_of_request_observations",
            "planned_requests": len(requests),
            "counts": counts,
            "requests": outcomes,
            "scope": "selected_category_or_all_categories_qualified_observed_windows_only",
            "limitations": [
                "no_sum_of_partial_cpu_or_host_swap_windows",
                "not_continuous_resource_bound",
                "no_missing_request_exclusion",
            ],
        },
    }
