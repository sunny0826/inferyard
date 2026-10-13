"""Client timing reductions; each request remains one observation, never one token."""

import math
from collections import Counter

from inferyard.analysis.engine_timing import engine_rates


def distribution(values, excluded=0):
    values = sorted(values)
    count = len(values)
    return {
        "sample_count": count,
        "excluded": excluded,
        "min": values[0] if count else None,
        "p50": values[math.ceil(count * 0.5) - 1] if count else None,
        "p95": values[math.ceil(count * 0.95) - 1] if count >= 20 else None,
        "max": values[-1] if count else None,
        "quantile_method": "nearest_rank.phase2.v1",
        "p95_reason": "insufficient_samples" if count < 20 else None,
        "p95_exploratory": 20 <= count < 100,
    }


def request_timing(row):
    start, end = row.get("t_send_ns"), row.get("t_terminal_ns")
    valid = type(start) is int and type(end) is int and 0 <= start <= end
    result = {}
    for code, key, missing in (
        ("L01", "t_first_content_ns", "no_content_event"),
        ("L02", "t_first_answer_ns", "no_answer_event"),
        ("L03", "t_terminal_ns", "evidence_missing"),
    ):
        observed = row.get(key)
        ok = valid and type(observed) is int and start <= observed <= end
        result[code] = {
            "value": (observed - start) / 1e6 if ok else None,
            "unit": "ms",
            "reason": None if ok else missing if valid and observed is None else "invalid_timing",
        }
    tokens = row.get("completion_tokens")
    usage = (
        row.get("token_source") == "endpoint.usage"
        and row.get("token_scope") == "completion_tokens"
    )
    ok = valid and end > start and type(tokens) is int and tokens >= 0 and usage
    result["L04"] = {
        "value": tokens * 1e9 / (end - start) if ok else None,
        "unit": "token/s",
        "reason": None if ok else "usage_or_positive_duration_missing",
        "source": row.get("token_source"),
        "scope": row.get("token_scope"),
    }
    capture, arrivals = row.get("arrival_capture"), row.get("block_arrivals", [])
    reason = (
        "evidence_missing"
        if capture is None
        else "not_streaming"
        if not capture["streaming"]
        else "insufficient_blocks"
        if len(arrivals) < 2
        else None
    )
    intervals = (
        [
            (b["monotonic_ns"] - a["monotonic_ns"]) / 1e6
            for a, b in zip(arrivals, arrivals[1:], strict=False)
        ]
        if reason is None
        else []
    )
    result["L05"] = {
        "unit": "ms",
        "reason": reason,
        "intervals": intervals,
        "distribution": distribution(intervals),
        "source": "decoded_delta" if capture else None,
    }
    result.update(engine_rates(row))
    return result


def summarize_performance(requests):
    """Separate categories within this trial; do not pool failed requests into latency."""
    groups = {}
    for category in sorted({r["category"] for r in requests}):
        rows = [r for r in requests if r["category"] == category]
        completed = [r for r in rows if r["execution_state"] == "completed"]
        timings = [request_timing(r) for r in completed]
        metrics = {}
        for code in ("L01", "L02", "L03", "L04", "L06", "L07"):
            values = [t[code]["value"] for t in timings if t[code]["value"] is not None]
            metrics[code] = {
                **distribution(values, len(rows) - len(values)),
                "unit": "token/s" if code in ("L04", "L06", "L07") else "ms",
                "missing_reasons": dict(
                    Counter(t[code]["reason"] for t in timings if t[code]["reason"])
                ),
            }
        # Per-request block distributions retain equal request identity and weighting.
        metrics["L05"] = [
            {"request_id": r["request_id"], **t["L05"]}
            for r, t in zip(completed, timings, strict=True)
        ]
        failed = [r for r in rows if r["execution_state"] == "failed"]
        groups[category] = {
            "metrics": metrics,
            "planned": len(rows),
            "completed": len(completed),
            "failed": len(failed),
            "timeout_count": sum(r.get("error_category") == "total_timeout" for r in failed),
            "failure_categories": dict(Counter(r.get("error_category") for r in failed)),
            "failed_first_events": [
                {
                    "request_id": r["request_id"],
                    **{k: v for k, v in request_timing(r).items() if k in ("L01", "L02")},
                }
                for r in failed
            ],
        }
    return groups
