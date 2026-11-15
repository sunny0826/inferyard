"""Dashboard cards for Studio; raw denominators and sample scopes stay intact."""

from inferyard.analysis.performance import distribution, request_timing

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


def dashboard_view(data, memory):
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
    }
