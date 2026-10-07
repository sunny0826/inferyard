"""Bind observed residual CPU intervals to requests and a separately frozen tolerance."""


def assess_windows(reduction, requests, policy):
    rows = []
    for request in requests:
        left, right = request.get("t_send_ns"), request.get("t_terminal_ns")
        reasons, values, spans = set(), [], []
        if policy is None:
            reasons.add("external_load_tolerance_not_frozen")
        if type(left) is not int or type(right) is not int or right <= left:
            reasons.add("request_interval_unavailable")
            coverage, duration = 0, None
        else:
            duration = right - left
            for interval in reduction["intervals"]:
                a, b = interval["start_ns"], interval["end_ns"]
                if a is None or b <= left or a >= right:
                    continue
                if interval["value_percent"] is None:
                    continue
                if policy and b - a > policy["max_external_interval_seconds"] * 1e9:
                    reasons.add("external_load_interval_too_wide")
                    continue
                values.append(interval["value_percent"])
                spans.append((max(a, left), min(b, right)))
            coverage, cursor = 0, left
            for a, b in sorted(spans):
                if b > max(cursor, a):
                    coverage += b - max(cursor, a)
                cursor = max(cursor, b)
            if coverage != duration:
                reasons.add("external_load_request_coverage_incomplete")
            if policy and any(v > policy["max_external_cpu_percent"] for v in values):
                reasons.add("external_load_frozen_tolerance_exceeded")
        rows.append(
            {
                "request_id": request["request_id"],
                "execution_state": request["execution_state"],
                "covered_ns": coverage,
                "duration_ns": duration,
                "coverage_ratio": coverage / duration if duration else None,
                "observed_max_percent": max(values, default=None),
                "observed_intervals": len(values),
                "eligible": not reasons,
                "reasons": sorted(reasons),
            }
        )
    return {
        "policy": policy,
        "requests": rows,
        "all_requests_eligible": bool(rows) and all(r["eligible"] for r in rows),
        "limitations": [
            "threshold_applies_to_bracketing_interval_averages_not_instantaneous_load",
            "cross_request_intervals_are_not_independent_observations",
            "not_full_environment_or_performance_comparison_authorization",
        ],
    }
