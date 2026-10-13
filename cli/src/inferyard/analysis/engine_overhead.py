"""Separately frozen qualification of pinned engine rates and processed work."""

import math
from statistics import median

from inferyard.analysis.engine_timing import DEFINITION, engine_rates
from inferyard.evidence.storage import EvidenceError

CODES = ("L06", "L07")
CONTRACT = "engine_rate_abba.v1"


def positive(value):
    return type(value) in (int, float) and math.isfinite(value) and value > 0


def contract(tolerance):
    if (
        type(tolerance) not in (int, float)
        or not math.isfinite(tolerance)
        or not 0 <= tolerance < 1
    ):
        raise EvidenceError("invalid_engine_rate_overhead_tolerance")
    return {
        "definition": CONTRACT,
        "metric_ids": list(CODES),
        "timing_definition": DEFINITION,
        "tolerance_ratio": tolerance,
        "statistic": "max_absolute_adjacent_pair_case_median_relative_rate_change",
        "work_policy": "equal_processed_and_cache_tokens_per_case_across_four_arms",
        "missing_policy": "every_case_in_every_arm_requires_positive_verified_engine_rate",
    }


def projection(row):
    return {
        code: {
            key: rate[key]
            for key in ("value", "source", "processed_tokens", "cache_tokens", "reason")
        }
        for code, rate in engine_rates(row).items()
    }


def work(rate):
    if (
        rate.get("source") != DEFINITION
        or rate.get("reason") is not None
        or not positive(rate.get("value"))
        or type(rate.get("processed_tokens")) is not int
        or rate["processed_tokens"] <= 0
        or type(rate.get("cache_tokens")) is not int
        or rate["cache_tokens"] < 0
    ):
        return None
    return rate["processed_tokens"], rate["cache_tokens"]


def assess(protocol, trials):
    frozen = protocol["engine_rate_contract"]
    results = {}
    for code in CODES:
        rows = [[r.get("engine_rates", {}).get(code, {}) for r in t["requests"]] for t in trials]
        valid = len(rows) == 4 and all(
            len(r) == len(protocol["case_ids"]) and all(work(v) is not None for v in r)
            for r in rows
        )
        matched = valid and all(
            len({work(arm[i]) for arm in rows}) == 1 for i in range(len(protocol["case_ids"]))
        )
        pairs = []
        if matched:
            for off, on in ((0, 1), (3, 2)):
                changes = [
                    (b["value"] - a["value"]) / a["value"]
                    for a, b in zip(rows[off], rows[on], strict=True)
                ]
                pairs.append(
                    {
                        "off_run": off + 1,
                        "on_run": on + 1,
                        "case_relative_rate_changes": changes,
                        "median_relative_rate_change": median(changes),
                    }
                )
        worst = max((abs(p["median_relative_rate_change"]) for p in pairs), default=None)
        passed = matched and worst <= frozen["tolerance_ratio"]
        results[code] = {
            "definition": CONTRACT,
            "timing_definition": DEFINITION,
            "passed": passed,
            "tolerance_ratio": frozen["tolerance_ratio"],
            "pairs": pairs,
            "max_absolute_pair_median_change": worst,
            "reasons": []
            if passed
            else [
                "engine_rate_missing_or_invalid"
                if not valid
                else "engine_processed_or_cache_work_differs"
                if not matched
                else "engine_rate_perturbation_exceeds_frozen_tolerance"
            ],
        }
    return results


def pair_reasons(code, assessments, cohorts):
    reasons = []
    if any(a.get("passed") is not True for a in assessments):
        reasons.append("engine_rate_overhead_not_qualified_on_both_sides")
    if (
        any(
            a.get("definition") != CONTRACT or a.get("timing_definition") != DEFINITION
            for a in assessments
        )
        or assessments[0].get("tolerance_ratio") is None
        or assessments[0].get("tolerance_ratio") != assessments[1].get("tolerance_ratio")
    ):
        reasons.append("engine_rate_overhead_contract_mismatch")
    indexed = [{r["case_id"]: projection(r)[code] for r in rows} for rows in cohorts]
    if (
        not indexed[0]
        or indexed[0].keys() != indexed[1].keys()
        or any(len(rows) != len(index) for rows, index in zip(cohorts, indexed, strict=True))
        or any(
            work(indexed[0][case]) is None or work(indexed[0][case]) != work(indexed[1][case])
            for case in indexed[0].keys() & indexed[1].keys()
        )
    ):
        reasons.append("engine_target_processed_or_cache_work_unknown_or_different")
    return reasons
