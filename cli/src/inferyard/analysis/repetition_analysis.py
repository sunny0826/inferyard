"""Cross-run S03/S04 analysis records with explicit aggregate source identities."""

import hashlib

from inferyard import SCHEMA_VERSION
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.provenance import tool_source_hash


def make_analysis(group, runs, versions, evidence, *, producer=None):
    by_id = {data["run"]["run_id"]: data for data in runs}
    ids = [rid for rid in group["run_ids"] if rid is not None]
    if not ids:
        return None
    selected = [by_id[rid] for rid in ids]
    scorers = {d["selection"]["scorer_sha256"] for d in selected}
    policies = {
        hashlib.sha256(json_bytes(d["bundle"]["answer_policy"])).hexdigest() for d in selected
    }
    if len(scorers) != 1 or len(policies) != 1:
        raise EvidenceError("repeat_scoring_definition_mismatch")
    from inferyard.evidence.storage import sha256_file

    sources = [
        {"run_id": d["run"]["run_id"], "manifest_sha256": sha256_file(d["path"] / "manifest.json")}
        for d in selected
    ]
    identity = hashlib.sha256(
        json_bytes({"group": group, "sources": sources, "tool": producer or tool_source_hash()})
    ).hexdigest()
    aid = "repeat-" + identity[:32]
    metrics = []

    def add(
        code,
        statistic,
        value,
        *,
        unit,
        count,
        reason=None,
        numerator=None,
        denominator=None,
        source="frozen_repeat_ledger",
        excluded=0,
    ):
        item = {
            "schema_version": SCHEMA_VERSION,
            "metric_id": code,
            "definition_version": "phase2.v1",
            "analysis_id": aid,
            "source_run_ids": ids,
            "run_id": None,
            "trial_id": None,
            "request_id": None,
            "group": {
                "workload_id": group["workload_id"],
                "category": None,
                "error_category": None,
            },
            "statistic": statistic,
            "value": value,
            "unit": unit,
            "layer": "experiment",
            "source": source,
            "status": "missing" if value is None else "derived",
            "missing_reason": (reason or "incomplete_repeat_evidence") if value is None else None,
            "sample_count": count,
            "numerator": numerator,
            "denominator": denominator,
            "excluded": excluded,
            "sampled_start_ns": None,
            "sampled_end_ns": None,
            "coverage_ratio": None,
            "comparison_eligible": False,
            "limitations": [*group["limitations"], "cross_run_no_shared_monotonic_interval"],
            "evidence_refs": evidence,
        }
        validate_document("metric_observation", item)
        metrics.append(item)

    quality = group["S03"]
    add(
        "S03",
        "all_planned_repeats_pass_rate",
        quality["value"],
        unit="ratio",
        count=quality["denominator"] if quality["value"] is not None else 0,
        numerator=quality["numerator"],
        denominator=quality["denominator"] if quality["numerator"] is not None else None,
        reason=quality["reason"],
    )
    perf = group["S04"]
    observed = sum(v is not None for v in perf["trial_medians_ms"])
    for statistic, key in (("trial_median_range", "range_ms"), ("trial_median_iqr", "iqr_ms")):
        add(
            "S04",
            statistic,
            perf[key],
            unit="ms",
            count=observed,
            reason=perf["reason"],
            excluded=group["expected_repeats"] - observed,
        )
    for index, value in enumerate(perf["trial_medians_ms"]):
        add(
            "S04",
            "trial_median",
            value,
            unit="ms",
            count=len(group["cases"]) if value is not None else 0,
            source=f"frozen_repeat_ledger:repeat={index}",
            reason=perf["reason"],
        )
    for case in group["cases"]:
        for index, value in enumerate(case["paired_latency_difference_ms"]):
            add(
                "S04",
                "paired_case_latency_difference",
                value,
                unit="ms",
                count=(1 if index == 0 else 2) if value is not None else 0,
                source=f"frozen_repeat_ledger:case={case['case_id']}:repeat={index}",
                reason=perf["reason"],
            )
    result = {
        "schema_version": SCHEMA_VERSION,
        "analysis_id": aid,
        "source_runs": sources,
        "parent_analysis_id": None,
        "reason": "frozen_repetition_summary",
        "definition_versions": versions,
        "scorer_id": versions["scoring"],
        "scorer_sha256": next(iter(scorers)),
        "answer_policy_sha256": next(iter(policies)),
        "metrics": metrics,
        "limitations": group["limitations"],
    }
    validate_document("analysis", result)
    return result
