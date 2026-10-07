"""Frozen repeat groups: no resumed fragments or repeated-case independence claims."""

import math
from statistics import median

from inferyard.evidence.storage import EvidenceError


def repetition_metrics(plan, runs):
    trial_ids = {t["trial_id"] for t in plan["trials"]}
    seen = set()
    for data in runs:
        run = data["run"]
        if (
            run["run_id"] in seen
            or run["trial_id"] not in trial_ids
            or run["plan_sha256"] != plan["plan_sha256"]
        ):
            raise EvidenceError("invalid_repetition_run_binding")
        seen.add(run["run_id"])
    groups = []
    for workload in plan["experiment"]["workloads"]:
        if workload["protocol"]["kind"] != "fixed" or workload["repeats"] < 2:
            continue
        trials = [t for t in plan["trials"] if t["workload_id"] == workload["workload_id"]]
        selected = []
        reasons = []
        for trial in trials:
            candidates = [
                d
                for d in runs
                if d["run"]["trial_id"] == trial["trial_id"] and d["run"]["relation"] != "resume"
            ]
            if len(candidates) != 1:
                reasons.append("missing_or_ambiguous_repeat")
                selected.append(None)
                continue
            data = candidates[0]
            selected.append(data)
            if data["summary"]["completeness"] != "complete":
                reasons.append("incomplete_repeat")
            ids = [r["case_id"] for r in data["requests"]]
            if ids != trial["case_order"]:
                raise EvidenceError("repetition_case_order_mismatch")
        cases = []
        for cid in workload["protocol"]["case_ids"]:
            rows = [
                next((r for r in d["requests"] if r["case_id"] == cid), None) if d else None
                for d in selected
            ]
            quality = [r["quality_state"] if r else "missing" for r in rows]
            latency = [
                (r["t_terminal_ns"] - r["t_send_ns"]) / 1e6
                if r
                and r["execution_state"] == "completed"
                and r.get("t_terminal_ns") is not None
                and r.get("t_send_ns") is not None
                and r["t_terminal_ns"] >= r["t_send_ns"]
                else None
                for r in rows
            ]
            cases.append(
                {
                    "case_id": cid,
                    "quality_applicable": next(
                        (r["category"] not in ("performance", "svg") for r in rows if r), None
                    ),
                    "quality_by_repeat": quality,
                    "all_repeats_passed": all(q == "pass" for q in quality),
                    "latency_ms_by_repeat": latency,
                    "paired_latency_difference_ms": [
                        v - latency[0]
                        if v is not None and latency[0] is not None and not reasons
                        else None
                        for v in latency
                    ],
                }
            )
        quality_cases = [c for c in cases if c["quality_applicable"] is not False]
        quality_ready = (
            bool(quality_cases)
            and not reasons
            and all(
                all(q in ("pass", "fail") for q in c["quality_by_repeat"]) for c in quality_cases
            )
        )
        numerator = sum(c["all_repeats_passed"] for c in quality_cases)
        medians = []
        for i in range(len(trials)):
            values = [c["latency_ms_by_repeat"][i] for c in cases]
            medians.append(
                median(values) if values and all(v is not None for v in values) else None
            )
        performance_ready = not reasons and all(v is not None for v in medians)
        ordered = sorted(medians) if performance_ready else []

        def quantile(p, values=ordered):
            return values[max(0, math.ceil(len(values) * p) - 1)]

        groups.append(
            {
                "workload_id": workload["workload_id"],
                "expected_repeats": workload["repeats"],
                "run_ids": [d["run"]["run_id"] if d else None for d in selected],
                "cases": cases,
                "S03": {
                    "numerator": numerator if quality_ready else None,
                    "denominator": len(quality_cases),
                    "value": numerator / len(quality_cases) if quality_ready else None,
                    "reason": None if quality_ready else "incomplete_or_unscorable_repeat_group",
                },
                "S04": {
                    "trial_medians_ms": medians,
                    "range_ms": max(ordered) - min(ordered) if ordered else None,
                    "iqr_ms": quantile(0.75) - quantile(0.25) if ordered else None,
                    "reason": None
                    if performance_ready
                    else "incomplete_or_unmatched_completed_cases",
                    "quantile_definition": "nearest_rank",
                    "comparison_eligible": False,
                },
                "limitations": sorted(
                    set(
                        reasons
                        + [
                            "repeats_are_not_independent_cases",
                            "observed_variation_not_population_confidence",
                            "environment_comparison_gate_pending",
                        ]
                    )
                ),
            }
        )
    return groups
