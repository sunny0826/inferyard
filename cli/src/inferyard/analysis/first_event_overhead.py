"""Predeclared ABBA qualification for client first-content/first-answer latency."""

import math
from statistics import median

from inferyard.evidence.storage import EvidenceError

FIELDS = {"L01": "t_first_content_ns", "L02": "t_first_answer_ns"}


def contract(tolerance):
    if (
        type(tolerance) not in (int, float)
        or not math.isfinite(tolerance)
        or not 0 <= tolerance < 1
    ):
        raise EvidenceError("invalid_first_event_overhead_tolerance")
    return {
        "definition": "first_event_abba.v1",
        "metric_ids": list(FIELDS),
        "tolerance_ratio": tolerance,
        "statistic": "max_absolute_adjacent_pair_case_median_relative_latency_change",
        "missing_policy": "every_case_in_every_arm_requires_positive_first_event_latency",
    }


def projection(row):
    start, end = row.get("t_send_ns"), row.get("t_terminal_ns")
    result = {}
    for code, field in FIELDS.items():
        event = row.get(field)
        valid = all(type(v) is int for v in (start, end, event)) and 0 <= start < event <= end
        result[code] = event - start if valid else None
    return result


def assess(protocol, trials):
    """Caller has validated complete ordered arms with identical output work."""
    frozen = protocol["first_event_contract"]
    results = {}
    for code in FIELDS:
        values = [[r.get("first_event_ns", {}).get(code) for r in t["requests"]] for t in trials]
        valid = len(values) == 4 and all(
            len(v) == len(protocol["case_ids"]) and all(type(x) is int and x > 0 for x in v)
            for v in values
        )
        pairs = []
        if valid:
            for off, on in ((0, 1), (3, 2)):
                changes = [(b - a) / a for a, b in zip(values[off], values[on], strict=True)]
                pairs.append(
                    {
                        "off_run": off + 1,
                        "on_run": on + 1,
                        "case_relative_latency_changes": changes,
                        "median_relative_latency_change": median(changes),
                    }
                )
        worst = max((abs(p["median_relative_latency_change"]) for p in pairs), default=None)
        passed = valid and worst <= frozen["tolerance_ratio"]
        results[code] = {
            "definition": frozen["definition"],
            "passed": passed,
            "tolerance_ratio": frozen["tolerance_ratio"],
            "pairs": pairs,
            "max_absolute_pair_median_change": worst,
            "reasons": []
            if passed
            else [
                "first_event_missing_invalid_or_zero"
                if not valid
                else "first_event_perturbation_exceeds_frozen_tolerance"
            ],
        }
    return results
