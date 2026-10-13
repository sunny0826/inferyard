"""Describe per-request resource observations within one actual-token cohort."""

from collections import Counter, defaultdict

from inferyard.analysis.performance import distribution


def resource_cohort(requests, observations):
    valid = {
        r["request_id"]
        for r in requests
        if r["request_id"] is not None and r["execution_state"] in ("completed", "failed")
    }
    groups = defaultdict(lambda: defaultdict(list))
    for item in observations:
        if item["request_id"] in valid and item["metric_id"].startswith("C"):
            key = tuple(
                item[k]
                for k in ("metric_id", "definition_version", "statistic", "unit", "layer", "source")
            )
            groups[key][item["request_id"]].append(item)
    result = []
    for key, by_request in sorted(groups.items()):
        values, reasons, limits = [], Counter(), set()
        for request in sorted(valid):
            matches = by_request.get(request, [])
            if len(matches) != 1:
                reasons["missing_observation" if not matches else "ambiguous_observation"] += 1
                continue
            item = matches[0]
            limits.update(item["limitations"])
            if item["value"] is None:
                reasons[item["missing_reason"] or "missing_reason_unspecified"] += 1
            else:
                values.append(item["value"])
        stats = distribution(values)
        result.append(
            {
                **dict(
                    zip(
                        ("metric_id", "definition_version", "statistic", "unit", "layer", "source"),
                        key,
                        strict=True,
                    )
                ),
                "valid_executed": len(valid),
                "observed_requests": len(values),
                "missing_requests": len(valid) - len(values),
                "missing_reasons": dict(sorted(reasons.items())),
                "min": stats["min"],
                "p50": stats["p50"],
                "max": stats["max"],
                "limitations": sorted(
                    limits
                    | {
                        "distribution_of_per_request_resource_observations",
                        "completed_and_failed_requests_included",
                        "not_whole_cohort_continuous_resource_measurement",
                        "no_cross_source_pooling",
                        "nearest_rank.phase2.v1",
                    }
                ),
            }
        )
    return {
        "series": result,
        "status": "available" if result else "missing",
        "missing_reason": None if result else "no_resource_observations_for_valid_requests",
    }
