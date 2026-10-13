"""Versioned numeric observations with explicit grouping and immutable file evidence."""

from collections import Counter

from inferyard import SCHEMA_VERSION
from inferyard.analysis.performance import distribution, request_timing
from inferyard.analysis.quality import summarize_quality
from inferyard.registry import catalogue


class Observations:
    def __init__(self, run, workload_id, evidence, *, complete):
        self.run, self.workload_id, self.evidence = run, workload_id, evidence
        self.complete = complete
        self.definitions = {m["metric_id"]: m for m in catalogue("metrics")["items"]}
        self.items = []

    def add(
        self,
        code,
        statistic,
        value,
        *,
        category=None,
        error_category=None,
        request=None,
        count=0,
        excluded=0,
        numerator=None,
        denominator=None,
        reason=None,
        source=None,
        limits=(),
        comparison=True,
        interval=None,
        not_applicable=False,
        unit=None,
        coverage=None,
    ):
        definition = self.definitions[code]
        limitations = list(limits)
        if not self.complete:
            limitations.append("incomplete_trial_subset_only")
        version_kind = "scoring" if code.startswith("Q") else "measurement"
        if self.run["definition_versions"][version_kind] != definition["definition_version"]:
            limitations.append("frozen_definition_differs_from_reducer")
            comparison = False
        if code.startswith(("L", "C")):
            # Pair comparability also needs environment, token scope and adapter checks.
            limitations.append("performance_comparison_gate_pending")
            comparison = False
        item = {
            "schema_version": SCHEMA_VERSION,
            "metric_id": code,
            "definition_version": definition["definition_version"],
            "run_id": self.run["run_id"],
            "trial_id": self.run["trial_id"],
            "request_id": request,
            "group": {
                "workload_id": self.workload_id,
                "category": category,
                "error_category": error_category,
            },
            "statistic": statistic,
            "value": value,
            "unit": unit or definition["unit"],
            "layer": definition["layer"],
            "source": source or definition["source"],
            "status": "not_applicable"
            if not_applicable
            else "missing"
            if value is None
            else "derived",
            "missing_reason": (reason or "no_samples") if value is None else None,
            "sample_count": count,
            "numerator": numerator,
            "denominator": denominator,
            "excluded": excluded,
            "sampled_start_ns": interval[0] if interval else None,
            "sampled_end_ns": interval[1] if interval else None,
            "coverage_ratio": coverage,
            "comparison_eligible": bool(value is not None and self.complete and comparison),
            "limitations": list(dict.fromkeys(limitations)),
            "evidence_refs": self.evidence,
        }
        # Structural validation happens once on the assembled summary, which fully
        # covers every metric_observation item; validating each item here would repeat it.
        self.items.append(item)
        return item


def execution_observations(output, rows, category):
    valid = [r for r in rows if r["execution_state"] in ("completed", "failed")]
    completed = [r for r in valid if r["execution_state"] == "completed"]
    failures = Counter(
        (r.get("error_category") or "unknown") for r in valid if r["execution_state"] == "failed"
    )
    denominator, excluded = len(valid), len(rows) - len(valid)
    rates = [
        ("R01", "completion_rate", len(completed), None),
        ("R02", "timeout_rate", failures["total_timeout"], None),
        (
            "R04",
            "output_budget_exhaustion_rate",
            sum(bool(r.get("budget_exhausted")) for r in valid),
            None,
        ),
        *[("R03", "failure_type_rate", count, error) for error, count in sorted(failures.items())],
    ]
    if not failures:
        rates.append(("R03", "failure_type_rate", 0, "none_observed"))
    for code, statistic, numerator, error in rates:
        output.add(
            code,
            statistic,
            numerator / denominator if denominator else None,
            category=category,
            error_category=error,
            count=denominator,
            excluded=excluded,
            numerator=numerator,
            denominator=denominator,
            reason="zero_denominator",
        )


def quality_observations(output, cases, rows, category):
    if category == "svg":
        return
    if category == "performance":
        output.add(
            "Q01",
            "whole_case_pass_rate",
            None,
            category=category,
            reason="performance_task_without_quality_score",
            not_applicable=True,
        )
        return
    quality = summarize_quality(cases, rows, complete=output.complete)
    valid = sum(r["execution_state"] in ("completed", "failed") for r in rows)
    rates = {"Q01": quality["Q01"][category]["rate"]}
    if category == "instruction":
        rates["Q02"] = quality["Q02"]
    if category in ("extraction", "structured"):
        rates.update({code: quality[code] for code in ("Q03", "Q04", "Q05")})
    if category in ("qa", "math"):
        code = "Q06" if category == "qa" else "Q07"
        rates[code] = quality[code]
    for code, rate in rates.items():
        output.add(
            code,
            "pass_rate",
            rate["value"],
            category=category,
            count=valid,
            excluded=len(rows) - valid,
            numerator=rate["numerator"],
            denominator=rate["denominator"],
            reason=rate["reason"],
            limits=["denominator_is_constraints"]
            if code == "Q02"
            else ["denominator_is_frozen_fields"]
            if code == "Q05"
            else [],
        )
    if category == "classification":
        metric = quality["Q08"]
        output.add(
            "Q08",
            "macro_f1",
            metric["value"],
            category=category,
            count=metric["sample_count"],
            excluded=len(rows) - valid,
            reason=metric["reason"],
            limits=["fixed_labels_zero_division_zero", "confusion_matrix_in_quality_summary"],
        )


def timing_observations(output, rows, category):
    timings = [(r, request_timing(r)) for r in rows if r.get("request_id")]
    for row, metrics in timings:
        success = row["execution_state"] == "completed"
        interval = (
            (row["t_send_ns"], row["t_terminal_ns"])
            if row.get("t_send_ns") is not None and row.get("t_terminal_ns") is not None
            else None
        )
        for code in ("L01", "L02", "L03", "L04", "L06", "L07"):
            metric = metrics[code]
            diagnostic = not success and code in ("L01", "L02")
            value = metric["value"] if success or diagnostic else None
            output.add(
                code,
                "failed_first_event" if diagnostic else "request_value",
                value,
                category=category,
                request=row["request_id"],
                count=int(value is not None),
                reason=metric["reason"] if success or diagnostic else "request_not_completed",
                source="endpoint.usage/completion_tokens_over_e2e"
                if code == "L04"
                else metric["source"]
                if code in ("L06", "L07")
                else "client_monotonic_events",
                comparison=success,
                interval=None if code in ("L06", "L07") else interval,
                limits=["diagnostic_not_completion_distribution"]
                if diagnostic
                else ["uncached_prompt_tokens", "engine_prompt_time_includes_first_sampling"]
                if code == "L06"
                else ["first_token_excluded", "engine_samples_include_reasoning_and_special_tokens"]
                if code == "L07"
                else [],
            )
        block = metrics["L05"]
        for statistic in ("min", "p50", "p95", "max"):
            stats = block["distribution"]
            value = stats[statistic] if success else None
            reason = (
                "request_not_completed"
                if not success
                else block["reason"] or (stats["p95_reason"] if statistic == "p95" else None)
            )
            output.add(
                "L05",
                "within_request_" + statistic,
                value,
                category=category,
                request=row["request_id"],
                count=stats["sample_count"],
                reason=reason,
                interval=interval,
                source="decoded_delta_arrivals",
                limits=[
                    "sample_count_is_adjacent_block_intervals",
                    "not_token_itl",
                    "nearest_rank.phase2.v1",
                ],
            )
    for code in ("L01", "L02", "L03", "L04", "L06", "L07"):
        values = [
            m[code]["value"]
            for r, m in timings
            if r["execution_state"] == "completed" and m[code]["value"] is not None
        ]
        stats = distribution(values)
        missing = Counter(
            m[code]["reason"]
            for r, m in timings
            if r["execution_state"] == "completed" and m[code]["reason"]
        )
        for statistic in ("min", "p50", "p95", "max"):
            output.add(
                code,
                statistic,
                stats[statistic],
                category=category,
                count=len(values),
                excluded=len(rows) - len(values),
                reason="insufficient_samples"
                if statistic == "p95" and len(values) < 20
                else "no_valid_completed_samples",
                limits=[
                    "nearest_rank.phase2.v1",
                    *[f"missing:{key}:{count}" for key, count in sorted(missing.items())],
                    *(
                        ["exploratory_p95"]
                        if statistic == "p95" and stats["p95_exploratory"]
                        else []
                    ),
                ],
            )


def build_observations(run, workload_id, cases, requests, evidence, *, complete, repeated=False):
    output = Observations(run, workload_id, evidence, complete=complete)
    by_id = {c["case_id"]: c for c in cases}
    for category in sorted({r["category"] for r in requests}):
        rows = [r for r in requests if r["category"] == category]
        execution_observations(output, rows, category)
        if not repeated:
            quality_observations(output, [by_id[r["case_id"]] for r in rows], rows, category)
        timing_observations(output, rows, category)
    if repeated:
        for item in output.items:
            item["limitations"].append("repeated_probes_not_independent_cases")
    return output.items
