"""Window-local stability observations, with repeated probes grouped by case.

Requests belong to their send-time cohort. Their full terminal latency remains
in that cohort even when they finish in a later window or the drain period.
"""

from collections import Counter
from statistics import median

from inferyard.evidence.storage import EvidenceError


def summarize_windows(protocol, case_order, start_ns, requests):
    width = int(protocol["window_seconds"] * 1e9)
    duration = int(protocol["duration_seconds"] * 1e9)
    if width <= 0 or duration <= 0 or not case_order:
        raise EvidenceError("invalid_duration_window")
    if len(set(case_order)) != len(case_order):
        raise EvidenceError("duplicate_duration_probe")
    if len({r["request_id"] for r in requests}) != len(requests):
        raise EvidenceError("duplicate_duration_attempt")
    if any(r["case_id"] not in case_order for r in requests):
        raise EvidenceError("unknown_duration_probe")
    windows = []
    baseline = {}
    for index, left in enumerate(range(start_ns, start_ns + duration, width)):
        right = min(left + width, start_ns + duration)
        groups = []
        for case in case_order:
            candidates = [
                r
                for r in requests
                if r["case_id"] == case
                and r.get("t_send_ns") is not None
                and left <= r["t_send_ns"] < right
            ]
            rows = [
                r
                for r in candidates
                if r.get("t_terminal_ns") is not None and r["t_send_ns"] <= r["t_terminal_ns"]
            ]
            completed = [r for r in rows if r["execution_state"] == "completed"]
            enough = len(completed) >= protocol["min_completed_per_case_per_window"]
            latency = (
                median(r["t_terminal_ns"] - r["t_send_ns"] for r in completed) if enough else None
            )
            # Never move the baseline to a later, more convenient window.
            if index == 0:
                baseline[case] = latency
            reference = baseline.get(case)
            drift = latency / reference - 1 if latency is not None and reference else None
            groups.append(
                {
                    "case_id": case,
                    "started": len(candidates),
                    "completed_in_send_cohort": len(completed),
                    "unfinished_or_invalid_timing": len(candidates) - len(rows),
                    "cross_boundary_completed": sum(r["t_terminal_ns"] > right for r in completed),
                    "failed": sum(r["execution_state"] == "failed" for r in candidates),
                    "valid_executed": sum(
                        r["execution_state"] in ("completed", "failed") for r in candidates
                    ),
                    # Timing eligibility must not remove interrupted attempts
                    # from the execution ledger for this send-time cohort.
                    "execution_states": dict(Counter(r["execution_state"] for r in candidates)),
                    "median_latency_ns": latency,
                    "baseline_latency_ns": reference,
                    "latency_drift_ratio": drift,
                    "latency_difference_ns": latency - reference
                    if latency is not None and reference is not None
                    else None,
                    "missing_reason": "insufficient_completed_per_case"
                    if not enough
                    else "initial_window_baseline_unavailable"
                    if not reference
                    else None,
                }
            )
        windows.append(
            {
                "index": index,
                "start_ns": left,
                "end_ns": right,
                "full_width": right - left == width,
                "cases": groups,
            }
        )
    return {
        "windows": windows,
        "independent_cases": len(case_order),
        "limitations": [
            "repeated_probes_are_not_independent_cases",
            "send_time_cohorts_include_full_cross_window_latency",
            "observed_latency_drift_is_not_causal_throttling_evidence",
        ],
    }
