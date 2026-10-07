"""Contextual metric comparisons; original run observations remain unchanged."""

import math
from collections import defaultdict

from inferyard.analysis.environment_identity import fields


def finite(value):
    return type(value) in (int, float) and math.isfinite(value)


def metric_index(data):
    requests = {r["request_id"]: r for r in data.get("requests", [])}
    indexed = defaultdict(list)
    for metric in data["summary"].get("metric_observations", []):
        if not metric["metric_id"].startswith(("L", "C")):
            continue
        request_id = metric.get("request_id")
        case = requests.get(request_id, {}).get("case_id") if request_id else None
        key = (
            metric["metric_id"],
            metric["statistic"],
            metric["group"]["category"],
            case,
            metric["source"],
        )
        indexed[key].append(metric)
    return indexed


def performance_comparison(left, right, comparison, evidence, *, definition="phase2.v3"):
    from inferyard.evidence.formats import require_version

    require_version({"definition": definition}, "definition", ("phase2.v3",), "comparison")
    reasons = list(comparison["blockers"])
    for side, data in (("left", left), ("right", right)):
        if data["run"]["origin"] == "migrated":
            reasons.append(side + ":migrated_measurement_not_performance_qualified")
    for condition in comparison["conditions"]:
        if condition["impact"] == "performance" and condition["status"] not in (
            "same",
            "allowed_difference",
        ):
            reasons.append(condition["field"] + ":" + condition["status"])
    for side, data, proof in zip(("left", "right"), (left, right), evidence, strict=True):
        if proof["target_run_id"] != data["run"]["run_id"]:
            reasons.append(side + ":overhead_target_binding_mismatch")
        if proof["eligible"] is not True:
            reasons.append(side + ":overhead_evidence_not_qualified")
            reasons.extend(side + ":" + reason for reason in proof["reasons"])
        if proof.get("total_observer_binding", {}).get("eligible") is not True:
            reasons.append(side + ":formal_trial_total_observer_not_qualified")
        if (
            not data["summary"]
            .get("measurement_context", {})
            .get("environment_qualification", {})
            .get("eligible")
        ):
            reasons.append(side + ":environment_not_qualified")
        trial = next(t for t in data["plan"]["trials"] if t["trial_id"] == data["run"]["trial_id"])
        workload = next(
            w
            for w in data["plan"]["experiment"]["workloads"]
            if w["workload_id"] == trial["workload_id"]
        )
        if workload["protocol"]["kind"] != "fixed":
            reasons.append(side + ":duration_cohort_comparison_pending")
    for field in fields(
        [d.get("environment_start", {}) for d in (left, right)], include_policy=True
    ):
        a, b = (data.get("environment_start", {}).get(field) for data in (left, right))
        if a is None or b is None or a != b:
            reasons.append("pair_environment_unknown_or_different:" + field)
    same_tokenizer = all(
        left["config"]["model"].get(key) is not None
        and left["config"]["model"][key] == right["config"]["model"].get(key)
        for key in ("sha256", "template_sha256")
    )
    if not same_tokenizer:
        from inferyard.analysis.quantization_comparison import verified_tokenizer_equal

        same_tokenizer = verified_tokenizer_equal(
            left, right, comparison.get("quantization_qualification")
        )
    indices = [metric_index(data) for data in (left, right)]
    first_events = [p.get("assessment", {}).get("first_event_assessments", {}) for p in evidence]
    first_event_mode = any(first_events)
    engine_rates = [p.get("assessment", {}).get("engine_rate_assessments", {}) for p in evidence]
    engine_rate_mode = any(engine_rates)
    block_gaps = [p.get("assessment", {}).get("block_gap_assessments", {}) for p in evidence]
    block_gap_mode = any(block_gaps)
    resource_policies = [d["plan"]["experiment"].get("resource_comparison") for d in (left, right)]
    resource_mode = any(p is not None for p in resource_policies)
    rows = []
    for key in sorted(set(indices[0]) | set(indices[1]), key=str):
        groups = [index.get(key, []) for index in indices]
        metrics = [group[0] if len(group) == 1 else None for group in groups]
        local = []
        resource_windows = []
        if any(len(group) != 1 for group in groups):
            local.append("missing_or_ambiguous_metric_pair")
        code, statistic, category, case, source = key
        if code in ("L01", "L02") and first_event_mode:
            assessments = [s.get(code, {}) for s in first_events]
            if any(a.get("passed") is not True for a in assessments):
                local.append("first_event_overhead_not_qualified_on_both_sides")
            if (
                assessments[0].get("definition") != "first_event_abba.v1"
                or assessments[1].get("definition") != "first_event_abba.v1"
                or assessments[0].get("tolerance_ratio") is None
                or assessments[0].get("tolerance_ratio") != assessments[1].get("tolerance_ratio")
            ):
                local.append("first_event_overhead_contract_mismatch")
        elif code in ("L06", "L07") and engine_rate_mode:
            from inferyard.analysis.engine_overhead import pair_reasons
            from inferyard.analysis.engine_timing import DEFINITION

            cohorts = [
                [
                    r
                    for r in data.get("requests", [])
                    if r["category"] == category and (case is None or r["case_id"] == case)
                ]
                for data in (left, right)
            ]
            local.extend(pair_reasons(code, [s.get(code, {}) for s in engine_rates], cohorts))
            if source != ("verified_engine_timings" if case is None else DEFINITION):
                local.append("engine_timing_source_not_verified")
        elif code == "L05" and block_gap_mode:
            from inferyard.analysis.block_overhead import pair_reasons

            cohorts = [
                [
                    r
                    for r in data.get("requests", [])
                    if r["category"] == category and r["case_id"] == case
                ]
                for data in (left, right)
            ]
            local.extend(
                pair_reasons(statistic, [s.get(statistic, {}) for s in block_gaps], cohorts)
            )
            if case is None or source != "decoded_delta_arrivals":
                local.append("block_gap_requires_per_request_decoded_arrivals")
        elif code.startswith("C") and resource_mode:
            from inferyard.analysis.resource_comparison import compare_windows

            cohorts = [
                [
                    r
                    for r in data.get("requests", [])
                    if r["category"] == category and r["case_id"] == case
                ]
                for data in (left, right)
            ]
            refused, resource_windows = compare_windows(
                left, right, metrics, cohorts, resource_policies
            )
            local.extend(refused)
        elif code not in ("L03", "L04"):
            local.append("metric_specific_comparison_qualification_pending")
        if (
            code == "L04"
            or (engine_rate_mode and code in ("L06", "L07"))
            or (block_gap_mode and code == "L05")
        ) and not same_tokenizer:
            local.append("tokenizer_identity_not_proven_equal")
        for side_index, (data, metric) in enumerate(zip((left, right), metrics, strict=True)):
            if metric is None:
                continue
            if not finite(metric.get("value")) or metric.get("missing_reason") is not None:
                local.append("metric_missing_or_nonfinite")
            limits = metric.get("limitations", [])
            if any(
                x in limits
                for x in ("incomplete_trial_subset_only", "frozen_definition_differs_from_reducer")
            ):
                local.append("metric_definition_or_completeness_limited")
            if (
                not metric.get("comparison_eligible")
                and "performance_comparison_gate_pending" not in limits
            ):
                local.append("metric_not_locally_eligible")
            cohort = [
                r
                for r in data.get("requests", [])
                if r["category"] == category and (case is None or r["case_id"] == case)
            ]
            expected_count = len(cohort)
            resource = code.startswith("C") and resource_mode
            if resource:
                expected_count = (
                    resource_windows[side_index].get("samples")
                    if len(resource_windows) == 2
                    else None
                )
            if code == "L05" and block_gap_mode:
                from inferyard.analysis.block_overhead import projection

                expected_count = projection(cohort[0])["gap_count"] if len(cohort) == 1 else None
            if (
                not cohort
                or any(r["execution_state"] != "completed" for r in cohort)
                or metric.get("sample_count") != expected_count
                or (not resource and metric.get("excluded", 0) != 0)
            ):
                local.append("metric_cohort_incomplete")
            if code == "L04" and any(
                r.get("token_source") != "endpoint.usage"
                or r.get("token_scope") != "completion_tokens"
                for r in cohort
            ):
                local.append("output_token_scope_mismatch")
        a, b = metrics
        if a and b and any(a[k] != b[k] for k in ("definition_version", "unit", "layer", "source")):
            local.append("metric_definition_or_unit_mismatch")
        allowed = not reasons and not local
        rows.append(
            {
                "metric_id": code,
                "statistic": statistic,
                "category": category,
                "case_id": case,
                "source": source,
                "unit": a["unit"] if a else b["unit"] if b else None,
                "left": a["value"] if a and finite(a["value"]) else None,
                "right": b["value"] if b and finite(b["value"]) else None,
                "difference": b["value"] - a["value"] if allowed else None,
                "eligible": allowed,
                "reasons": sorted(set(local)),
                "left_limits": a.get("limitations", []) if a else [],
                "right_limits": b.get("limitations", []) if b else [],
                **(
                    {"resource_windows": resource_windows}
                    if code.startswith("C") and resource_mode
                    else {}
                ),
            }
        )
    result = {
        "definition": "contextual_block_gap_comparison.v1"
        if block_gap_mode
        else "contextual_engine_rate_comparison.v1"
        if engine_rate_mode
        else "contextual_first_event_comparison.v1"
        if first_event_mode
        else "contextual_e2e_comparison.v1",
        "prerequisites_eligible": not reasons,
        "eligible": any(row["eligible"] for row in rows),
        "blockers": sorted(set(reasons)),
        "differences": rows,
        "scope": "per_metric_descriptive_right_minus_left",
        "limitations": [
            "engine_rates_and_first_events_require_separate_predeclared_overhead_qualification"
            if engine_rate_mode
            else "first_events_require_separate_predeclared_overhead_qualification"
            if first_event_mode
            else "only_e2e_and_same_tokenizer_e2e_output_rate_currently_qualified",
            "engine_rates_require_separate_predeclared_qualification_and_equal_processed_cache_work"
            if engine_rate_mode
            else "other_latency_components_engine_rates_and_resource_windows_remain_gated",
            *(["block_gaps_and_resource_windows_remain_gated"] if engine_rate_mode else []),
            "no_automatic_winner_causal_claim_or_population_confidence",
            "source_observation_eligibility_is_not_rewritten",
            "task_outputs_between_sides_may_differ",
        ],
    }
    if block_gap_mode:
        result["limitations"] = [
            "block_gaps_require_separate_predeclared_per_statistic_absolute_ms_tolerance",
            "equal_observed_channel_sequences_do_not_prove_equal_text_chunk_boundaries",
            "block_intervals_are_not_token_itl_or_independent_request_samples",
            "first_event_and_engine_rate_qualification_remain_separate",
            "resource_windows_remain_gated",
            "no_automatic_winner_causal_claim_or_population_confidence",
            "source_observation_eligibility_is_not_rewritten",
        ]
    if resource_mode:
        result["definition"] = "contextual_resource_windows.v1"
        result["limitations"] = [
            "resource_differences_are_for_frozen_matched_observed_windows_only",
            "sample_span_coverage_is_not_continuous_observation_or_true_extrema",
            "host_memory_swap_sensors_are_not_model_exclusive_attribution",
            "cpu_observed_intervals_do_not_become_full_request_totals",
            "latency_and_engine_metrics_keep_their_separate_qualification_gates",
            "no_automatic_winner_causal_claim_or_population_confidence",
            "source_observation_eligibility_is_not_rewritten",
        ]
    return result
