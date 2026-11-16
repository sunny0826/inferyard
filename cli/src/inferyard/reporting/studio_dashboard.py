"""Dashboard cards for Studio; raw denominators and sample scopes stay intact."""

from inferyard.analysis.performance import distribution, request_timing

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]

CATEGORIES = {
    "instruction": "指令遵循",
    "extraction": "信息提取",
    "qa": "知识问答",
    "math": "数学计算",
    "classification": "文本分类",
    "structured": "结构化输出",
}
COUNTS = ("numerator", "denominator", "excluded")
MIB = 1024**2


def _categories(summary):
    qualities = summary["quality"].get("Q01", {})
    rows, complete = [], []
    for key, label in CATEGORIES.items():
        values = qualities.get(key)
        if values is None:
            continue
        rate = values["rate"]
        rows.append(
            {"id": key, "label": label, "rate": {name: rate[name] for name in COUNTS}}
        )
        complete.append(rate["value"] is not None)
    return rows, bool(rows) and all(complete)


def _latency(requests):
    values = [
        request_timing(row)["L03"]["value"]
        for row in requests
        if row["execution_state"] == "completed"
    ]
    measured = [value for value in values if value is not None]
    stats = distribution(measured)
    return {
        "sample_count": stats["sample_count"],
        "excluded": len(requests) - len(measured),
        "min": stats["min"],
        "p50": stats["p50"],
        "p95": stats["p95"],
        "max": stats["max"],
    }


def _memory(memory, metric):
    sources = memory[metric]
    values = [value for value in sources.values() if value is not None]
    if memory["reason"] is not None:
        return None, memory["reason"]
    if len(sources) > 1:
        return None, "multiple_sources"
    if not values:
        return None, "resource_samples_missing"
    return values[0] * MIB, None


def _observation(
    summary: JsonObject, code: str, statistic: str
) -> JsonObject | None:
    return next(
        (
            metric
            for metric in summary.get("metric_observations", [])
            if metric["metric_id"] == code and metric["statistic"] == statistic
        ),
        None,
    )


def _throughput(summary: JsonObject) -> JsonObject:
    rows = {}
    for code, statistic, key in (
        ("N01", "requests_per_second", "requests_per_second"),
        ("N04", "output_tokens_per_second", "output_tokens_per_second"),
        ("N02", "goodput_per_second", "goodput_per_second"),
    ):
        metric = _observation(summary, code, statistic)
        rows[key] = metric["value"] if metric is not None else None
        rows[key + "_reason"] = (
            metric["missing_reason"] if metric is not None else "observation_missing"
        )
    return rows


def _pooled_metrics(summary: JsonObject) -> dict[str, JsonObject]:
    return {
        metric["statistic"]: metric
        for metric in summary.get("metric_observations", [])
        if metric["metric_id"] == "L05" and metric["statistic"].startswith("pooled_")
    }


def _interval_request_count(summary: JsonObject) -> int:
    return len(
        {
            metric["request_id"]
            for metric in summary.get("metric_observations", [])
            if metric["metric_id"] == "L05"
            and metric["statistic"].startswith("within_request_")
            and metric["value"] is not None
        }
    )


def _output_intervals(summary: JsonObject) -> JsonObject:
    pooled = _pooled_metrics(summary)
    anchor = pooled.get("pooled_min")
    request_count = _interval_request_count(summary)
    if anchor is None:
        return {
            "interval_count": 0,
            "request_count": request_count,
            "min": None,
            "p50": None,
            "p95": None,
            "p95_reason": "insufficient_samples",
            "max": None,
            "reason": "observation_missing",
        }
    count = anchor["sample_count"]
    return {
        "interval_count": count,
        **{
            statistic: pooled.get("pooled_" + statistic, {}).get("value")
            for statistic in ("min", "p50", "p95", "max")
        },
        "request_count": request_count,
        "p95_reason": "insufficient_samples" if count < 20 else None,
        "reason": anchor["missing_reason"],
    }


def dashboard_view(data: JsonObject, memory: JsonObject) -> JsonObject:
    categories, complete = _categories(data["summary"])
    quality = {
        "numerator": sum(row["rate"]["numerator"] for row in categories),
        "denominator": sum(row["rate"]["denominator"] for row in categories),
        "excluded": sum(row["rate"]["excluded"] for row in categories),
        "complete": complete,
        "reason": None if complete else "quality_incomplete_or_not_applicable",
    }
    peak, peak_reason = _memory(memory, "service_rss")
    available, available_reason = _memory(memory, "system_mem_available")
    return {
        "quality": quality,
        "categories": categories,
        "latency": _latency(data["requests"]),
        "sampled_peak_rss_bytes": peak,
        "sampled_peak_rss_reason": peak_reason,
        "sampled_minimum_available_bytes": available,
        "minimum_available_reason": available_reason,
        "throughput": _throughput(data["summary"]),
        "output_block_intervals": _output_intervals(data["summary"]),
    }
