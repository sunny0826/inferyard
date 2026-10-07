"""Single-run display reductions; raw quality denominators and sample scopes stay intact."""

from inferyard.analysis.performance import distribution, request_timing
from inferyard.reporting.svg_render import check_svg, extract_svg

CATEGORIES = {
    "instruction": "指令遵循",
    "extraction": "信息提取",
    "math": "数学计算",
    "qa": "知识问答",
    "classification": "文本分类",
    "structured": "结构化输出",
}


def request_details(request, case, ordinal, *, format_version=3):
    timing = request_timing(request)
    details = {
        "ordinal": ordinal,
        "category": request["category"],
        "category_label": CATEGORIES.get(request["category"], request["category"]),
        "reference_answer": case.get("reference_answer"),
        "rules": case["rules"],
        "rule_results": (request.get("score") or {}).get("rule_results", []),
        "first_answer_ms": timing["L02"]["value"],
        "completion_tokens": request.get("completion_tokens"),
        "token_source": request.get("token_source"),
        "finish_reason": request.get("finish_reason"),
        "reasoning": request.get("reasoning", ""),
    }
    if format_version >= 3 and case["category"] == "svg":
        details["svg_view"] = check_svg(extract_svg(request.get("content", "")))
    return details


def dashboard_view(data, resources):
    summary, requests = data["summary"], data["requests"]
    qualities = summary["quality"].get("Q01", {})
    categories = [
        {"id": key, "label": CATEGORIES.get(key, key), **values}
        for key, values in sorted(
            qualities.items(),
            key=lambda p: (list(CATEGORIES).index(p[0]) if p[0] in CATEGORIES else 99, p[0]),
        )
    ]
    numerator = sum(row["rate"]["numerator"] for row in categories)
    denominator = sum(row["rate"]["denominator"] for row in categories)
    quality_known = bool(categories) and all(row["rate"]["value"] is not None for row in categories)
    quality = {
        "numerator": numerator,
        "denominator": denominator,
        "excluded": sum(row["rate"]["excluded"] for row in categories),
        "complete": quality_known,
        "reason": None if quality_known else "quality_incomplete_or_not_applicable",
    }
    timings = [request_timing(row) for row in requests if row["execution_state"] == "completed"]
    latency = distribution([t["L03"]["value"] for t in timings if t["L03"]["value"] is not None])
    latency["excluded"] = len(requests) - latency["sample_count"]
    first = distribution([t["L02"]["value"] for t in timings if t["L02"]["value"] is not None])
    first["excluded"] = len(requests) - first["sample_count"]
    charts = resources["charts"]
    rss = [
        c["max"] * 1024**2 for c in charts if c["metric"] == "service_rss" and c["max"] is not None
    ]
    available = [
        c["min"] * 1024**2
        for c in charts
        if c["metric"] == "system_mem_available" and c["min"] is not None
    ]
    rss_sources = sorted({c["source"] for c in charts if c["metric"] == "service_rss"})
    memory_sources = sorted({c["source"] for c in charts if c["metric"] == "system_mem_available"})
    return {
        "quality": quality,
        "categories": categories,
        "latency": latency,
        "first_answer": first,
        "sampled_peak_rss_bytes": max(rss, default=None) if len(rss_sources) == 1 else None,
        "sampled_peak_rss_reason": (
            "multiple_sources"
            if len(rss_sources) > 1
            else "resource_samples_missing"
            if not rss
            else None
        ),
        "sampled_minimum_available_bytes": (
            min(available, default=None) if len(memory_sources) == 1 else None
        ),
        "minimum_available_reason": (
            "multiple_sources"
            if len(memory_sources) > 1
            else "resource_samples_missing"
            if not available
            else None
        ),
        "rss_sources": rss_sources,
        "available_memory_sources": memory_sources,
        "answer_failures": sum(row.get("quality_state") == "fail" for row in requests),
        "request_categories": [
            {"id": key, "label": CATEGORIES.get(key, key)}
            for key in sorted({row["category"] for row in requests})
        ],
    }
