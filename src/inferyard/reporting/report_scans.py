"""Length and position views project validated cohorts without pooling runs."""

from copy import deepcopy


def scan_view(summary, requests):
    bins = deepcopy(summary.get("input_lengths", {}).get("bins", []))
    known = [
        r
        for r in bins
        if r["actual_input_tokens"] is not None and r["completed_latency_p50_ms"] is not None
    ]
    maximum_x = max((r["actual_input_tokens"] for r in known), default=0) or 1
    maximum_y = max((r["completed_latency_p50_ms"] for r in known), default=0) or 1
    for row in bins:
        actual, latency = row["actual_input_tokens"], row["completed_latency_p50_ms"]
        row["point"] = (
            None
            if actual is None or latency is None
            else {
                "x": actual / maximum_x * 980 + 10,
                "y": 110 - latency / maximum_y * 100,
            }
        )
    observations = {}
    for item in summary.get("metric_observations", []):
        if item["metric_id"] == "X03" and item["request_id"] is not None:
            observations.setdefault((item["request_id"], item["statistic"]), []).append(item)

    def value(request, statistic):
        matches = observations.get((request, statistic), [])
        if len(matches) != 1:
            return {"value": None, "reason": "missing_or_ambiguous_observation"}
        return {"value": matches[0]["value"], "reason": matches[0]["missing_reason"]}

    output = [
        {
            "case_id": r["case_id"],
            "request_id": r["request_id"],
            "state": r["execution_state"],
            "actual": value(r["request_id"], "actual_output_tokens"),
            "budget": value(r["request_id"], "output_budget_tokens"),
            "target": value(r["request_id"], "declared_output_target_tokens"),
            "finish_reason": r.get("raw_finish_reason"),
            "error_category": r.get("error_category"),
        }
        for r in requests
    ]
    positions = deepcopy(summary.get("positions"))
    strict_targets = [
        deepcopy(m)
        for m in summary.get("metric_observations", [])
        if m["metric_id"] == "X03"
        and m["statistic"] == "strict_fixed_length_target_rate"
        and "strict_fixed_length_protocol" in m.get("limitations", [])
    ]
    return {
        "strict_output_targets": strict_targets,
        "length_bins": bins,
        "input_axis_max": maximum_x,
        "latency_axis_max": maximum_y,
        "positions": positions,
        "outputs": output,
        "limitations": [
            "observed_cohorts_not_capacity_claim",
            "no_interpolation_or_cross_run_pooling",
            "output_budget_is_not_actual_output_length",
        ],
    }
