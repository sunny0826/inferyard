"""X03 observations for separately frozen natural and strict output protocols."""

from collections import Counter

from inferyard.analysis.observations import Observations
from inferyard.runtime.fixed_output import enabled, target_rate


def output_budget_observations(run, workload, cases, requests, evidence, *, complete):
    output = Observations(run, workload["workload_id"], evidence, complete=complete)
    repeated = workload["protocol"]["kind"] == "duration"
    strict = enabled(workload)
    limits = [
        "natural_stop_protocol",
        "early_eos_is_not_execution_failure",
        "token_scope_and_performance_comparison_pending",
    ]
    if repeated:
        limits.append("repeated_probes_not_independent_cases")
    if strict:
        limits = [
            "strict_fixed_length_protocol",
            "target_equals_frozen_output_budget",
            "sampled_completion_tokens_not_visible_text_tokens",
            "performance_comparison_pending",
        ]
    by_id = {case["case_id"]: case for case in cases}

    def add(statistic, value, category, **kwargs):
        return output.add(
            "X03", statistic, value, category=category, comparison=False, limits=limits, **kwargs
        )

    for category in sorted({r["category"] for r in requests}):
        rows = [r for r in requests if r["category"] == category]
        valid = [r for r in rows if r["execution_state"] in ("completed", "failed")]
        denominator, excluded = len(valid), len(rows) - len(valid)
        reasons = Counter(r.get("raw_finish_reason") or "unreported" for r in valid)
        for reason, count in sorted(reasons.items()):
            add(
                "finish_reason_rate",
                count / denominator,
                category,
                count=denominator,
                numerator=count,
                denominator=denominator,
                excluded=excluded,
                source="length_case_ledger:finish=" + reason,
            )
        if not reasons:
            add(
                "finish_reason_rate",
                None,
                category,
                count=0,
                numerator=0,
                denominator=0,
                excluded=excluded,
                reason="no_valid_executed_requests",
            )
        exhausted = sum(r.get("budget_exhausted") is True for r in valid)
        known_ends = all(type(r.get("budget_exhausted")) is bool for r in valid)
        add(
            "output_budget_exhaustion_rate",
            exhausted / denominator if denominator and known_ends else None,
            category,
            count=denominator,
            numerator=exhausted if known_ends else None,
            denominator=denominator if known_ends else None,
            excluded=excluded,
            reason="terminal_budget_state_unknown",
        )
        if strict:
            add(
                "strict_fixed_length_target_rate",
                category=category,
                **target_rate(rows, workload["output_budget_tokens"], complete),
            )
        else:
            add(
                "strict_fixed_length_target_rate",
                None,
                category,
                count=0,
                reason="strict_fixed_length_protocol_not_enabled",
                not_applicable=True,
            )
        for row in rows:
            if row["request_id"] is None:
                continue
            request = row["request_id"]
            known = (
                row["execution_state"] in ("completed", "failed")
                and type(row.get("completion_tokens")) is int
                and row["completion_tokens"] >= 0
                and row.get("token_source") == "endpoint.usage"
                and row.get("token_scope") == "completion_tokens"
            )
            actual = row["completion_tokens"] if known else None
            add(
                "actual_output_tokens",
                actual,
                category,
                request=request,
                unit="tokens",
                count=int(known),
                reason="terminal_token_count_or_scope_unknown",
                source="endpoint.usage:completion_tokens",
            )
            add(
                "output_budget_tokens",
                workload["output_budget_tokens"],
                category,
                request=request,
                unit="tokens",
                count=1,
                source="frozen_workload",
            )
            target = by_id[row["case_id"]]["rules"].get("output_target_tokens")
            if strict:
                target = workload["output_budget_tokens"]
            add(
                "declared_output_target_tokens",
                target,
                category,
                request=request,
                unit="tokens",
                count=int(target is not None),
                reason="no_output_target_declared",
                not_applicable=target is None,
                source="frozen_strict_workload" if strict else "frozen_case",
            )
            add(
                "observed_target_reached",
                int(actual >= target) if known and target is not None else None,
                category,
                request=request,
                unit="boolean",
                count=int(known and target is not None),
                reason="no_output_target_declared"
                if target is None
                else "terminal_token_count_or_scope_unknown",
                not_applicable=target is None,
            )
    return output.items
