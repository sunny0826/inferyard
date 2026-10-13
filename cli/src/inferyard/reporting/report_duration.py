"""Display validated S01 observations without recomputing window statistics."""


def duration_view(summary):
    duration = summary.get("duration")
    if duration is None:
        return None
    metrics = {}
    for item in summary.get("metric_observations", []):
        if item["metric_id"] == "S01":
            metrics.setdefault((item["source"], item["statistic"]), []).append(item)

    def metric(source, statistic):
        matches = metrics.get((source, statistic), [])
        if len(matches) != 1:
            return {"value": None, "reason": "missing_or_ambiguous_observation"}
        item = matches[0]
        return {
            "value": item["value"],
            "reason": item["missing_reason"],
            "sample_count": item["sample_count"],
            "numerator": item["numerator"],
            "denominator": item["denominator"],
        }

    cases = {}
    origin = duration.get("start_ns")
    for window in duration.get("windows", []):
        for group in window["cases"]:
            case_id = group["case_id"]
            source = f"events.jsonl:case={case_id}:window={window['index']}"
            row = {
                "index": window["index"],
                "start_seconds": (window["start_ns"] - origin) / 1e9
                if origin is not None
                else None,
                "end_seconds": (window["end_ns"] - origin) / 1e9 if origin is not None else None,
                "started": group["started"],
                "cross_boundary_completed": group["cross_boundary_completed"],
                "latency": metric(source, "median_latency"),
                "drift": metric(source, "relative_change_from_first_window"),
                "failure": metric(source, "cohort_failure_rate"),
            }
            cases.setdefault(case_id, []).append(row)
    charts = []
    for case_id, rows in cases.items():
        maximum = (
            max(
                (r["latency"]["value"] for r in rows if r["latency"]["value"] is not None),
                default=0,
            )
            or 1
        )
        charts.append({"case_id": case_id, "rows": rows, "latency_max": maximum})
    return {
        "completed": duration["window_completed"],
        "reason": duration.get("reason"),
        "cases": charts,
        "window_count": len(duration.get("windows", [])),
    }
