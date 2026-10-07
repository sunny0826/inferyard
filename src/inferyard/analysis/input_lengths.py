"""Actual template-token cohorts; declared targets never stand in for measurements."""

import re
from collections import defaultdict

from inferyard.analysis.length_outcomes import outcome_cohort
from inferyard.analysis.length_resources import resource_cohort
from inferyard.analysis.observations import Observations
from inferyard.analysis.performance import distribution
from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.token_budgets import formal_budgets

TOKEN_SOURCE = "apply-template+tokenize:add_special,parse_special"


def bind_token_counts(selected, budgets, output_budget, *, source=TOKEN_SOURCE):
    if budgets is None:
        return {}
    result = {}
    # The archived inventory is probe, warmup, then frozen selected case order.
    for cid, budget in zip(selected, formal_budgets(budgets, selected), strict=True):
        if not isinstance(budget, dict):
            raise EvidenceError("invalid_template_token_record")
        known = (
            budget.get("verification") == "verified"
            and budget.get("source") == source
            and type(budget.get("input_tokens")) is int
            and budget["input_tokens"] >= 0
            and type(budget.get("output_budget")) is int
            and budget["output_budget"] == output_budget
            and isinstance(budget.get("template_prompt_sha256"), str)
            and re.fullmatch(r"[0-9a-f]{64}", budget["template_prompt_sha256"]) is not None
        )
        if known:
            result[cid] = {
                "actual_input_tokens": budget["input_tokens"],
                "template_prompt_sha256": budget["template_prompt_sha256"],
            }
    return result


def input_length_summary(
    run, workload, requests, counts, evidence, *, complete, resources=(), source=TOKEN_SOURCE
):
    output = Observations(run, workload["workload_id"], evidence, complete=complete)
    groups = defaultdict(list)
    repeated = workload["protocol"]["kind"] == "duration"
    limits = [
        "actual_template_token_cohorts_not_theoretical_context_limit",
        "acceptance_is_not_comprehension",
        "performance_comparison_pending",
    ]
    if repeated:
        limits.append("repeated_probes_not_independent_cases")
    for row in requests:
        record = counts.get(row["case_id"], {})
        actual = record.get("actual_input_tokens")
        groups[(row["category"], actual)].append(row)
        if row["request_id"] is not None:
            output.add(
                "X01",
                "actual_input_tokens",
                actual,
                unit="tokens",
                request=row["request_id"],
                category=row["category"],
                count=int(actual is not None),
                source=source,
                reason="verified_template_count_missing",
                comparison=False,
                limits=limits,
            )
    bins = []
    for (category, actual), rows in sorted(groups.items(), key=lambda item: str(item[0])):
        valid = [r for r in rows if r["execution_state"] in ("completed", "failed")]
        completed = [r for r in valid if r["execution_state"] == "completed"]
        failed = len(valid) - len(completed)
        unknown_scores = sum(
            (r.get("score") or {}).get("quality_state") not in ("pass", "fail")
            for r in completed
            if category not in ("performance", "svg")
        )
        passed = sum((r.get("score") or {}).get("quality_state") == "pass" for r in completed)
        times = [
            (r["t_terminal_ns"] - r["t_send_ns"]) / 1e6
            for r in completed
            if r.get("t_terminal_ns") is not None and r.get("t_send_ns") is not None
        ]
        observed = bool(valid) and complete
        quality_known = (
            observed
            and not unknown_scores
            and category not in ("performance", "svg")
            and not repeated
        )
        row = {
            "category": category,
            "actual_input_tokens": actual,
            "input_target_tokens": workload["input_target_tokens"],
            "output_budget_tokens": workload["output_budget_tokens"],
            "planned": None if repeated else len(rows),
            "attempts": len(rows),
            "valid_executed": len(valid),
            "completed": len(completed),
            "failed": failed,
            "excluded": len(rows) - len(valid),
            "unscorable_completed": unknown_scores,
            "completion_rate": len(completed) / len(valid) if observed else None,
            "quality_rate": passed / len(valid) if quality_known else None,
            "completed_latency_p50_ms": distribution(times)["p50"],
            "request_ids": [r["request_id"] for r in rows],
            "case_template_hashes": {
                r["case_id"]: counts.get(r["case_id"], {}).get("template_prompt_sha256")
                for r in rows
            },
            "limitations": limits,
            "resources": resource_cohort(rows, resources),
            "outcomes": outcome_cohort(rows),
        }
        bins.append(row)
        if actual is None:
            continue  # No inferred bucket from configured target length.
        for statistic, value, numerator in (
            ("completion_rate", row["completion_rate"], len(completed)),
            ("quality_rate", row["quality_rate"], passed),
        ):
            output.add(
                "X01",
                statistic,
                value,
                category=category,
                count=len(valid),
                numerator=numerator if value is not None else None,
                denominator=len(valid) if value is not None else None,
                excluded=row["excluded"],
                comparison=False,
                limits=limits,
                reason="incomplete_or_unscorable_length_cohort",
                source=f"length_case_ledger:input={actual}:budget={workload['output_budget_tokens']}",
            )
    return {"bins": bins, "limitations": limits}, output.items


def check_input_target(workload, selected, counts):
    target = workload["input_target_tokens"]
    tolerance = workload.get("input_tolerance_tokens", 0)
    if target is None:
        return {
            "status": "not_requested",
            "target_tokens": None,
            "tolerance_tokens": tolerance,
            "cases": [],
        }
    cases = []
    for cid in selected:
        actual = counts.get(cid, {}).get("actual_input_tokens")
        cases.append(
            {
                "case_id": cid,
                "actual_input_tokens": actual,
                "status": "unverified"
                if actual is None
                else "matched"
                if abs(actual - target) <= tolerance
                else "mismatch",
            }
        )
    status = (
        "unverified"
        if any(c["status"] == "unverified" for c in cases)
        else ("mismatch" if any(c["status"] == "mismatch" for c in cases) else "matched")
    )
    return {
        "status": status,
        "target_tokens": target,
        "tolerance_tokens": tolerance,
        "cases": cases,
    }
