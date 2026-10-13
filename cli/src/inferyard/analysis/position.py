"""Frozen character spans bind position variants to a shared question family."""

import hashlib
from collections import defaultdict

from inferyard.analysis.observations import Observations
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import json_bytes


def validate_position_cases(workload, bundle):
    records = workload.get("position_cases", [])
    if workload["purpose"] != "position":
        if records:
            raise ContractError("position_cases", "requires position purpose")
        return {}
    selected = workload["protocol"]["case_ids"]
    if len(records) != len(selected) or {r["case_id"] for r in records} != set(selected):
        raise ContractError("position_cases", "must cover each frozen case exactly once")
    cases = {c["case_id"]: c for c in bundle["cases"]}
    families = {}
    for record in records:
        case = cases[record["case_id"]]
        prompt = case["prompt"]
        if case["category"] in ("performance", "svg"):
            raise ContractError("position_cases", "requires a quality-scored case")
        if hashlib.sha256(prompt.encode()).hexdigest() != record["prompt_sha256"]:
            raise ContractError("position_cases.prompt_sha256", "differs from frozen text")
        start, end = record["body_start"], record["body_end"]
        if not 0 <= start < end <= len(prompt):
            raise ContractError("position_cases.body", "invalid character bounds")
        spans = record["evidence_spans"]
        if len(spans) != (1 if record["task_kind"] == "retrieval" else 2):
            raise ContractError("position_cases.evidence_spans", "wrong evidence count")
        previous = start
        for span in spans:
            if not previous <= span["start"] < span["end"] <= end:
                raise ContractError(
                    "position_cases.evidence_spans", "unordered or overlapping spans"
                )
            text = prompt[span["start"] : span["end"]]
            if hashlib.sha256(text.encode()).hexdigest() != span["sha256"]:
                raise ContractError("position_cases.evidence_spans", "evidence text hash mismatch")
            previous = span["end"]
        signature = json_bytes(
            {
                "category": case["category"],
                "rules": case["rules"],
                "reference_answer": case.get("reference_answer", ""),
                "answer_policy": bundle.get("answer_policy"),
                "prefix": prompt[:start],
                "suffix": prompt[end:],
                "kind": record["task_kind"],
                "evidence": [s["sha256"] for s in spans],
            }
        )
        if families.setdefault(record["family_id"], signature) != signature:
            raise ContractError(
                "position_cases.family_id", "family changes evidence or scoring rules"
            )
    return families


def register_position_families(workload, bundle, families):
    """A family retains question, evidence and scoring across every workload."""
    for family, signature in validate_position_cases(workload, bundle).items():
        if families.setdefault(family, signature) != signature:
            raise ContractError(
                "position_cases.family_id",
                "family changes question, evidence or scoring across workloads",
            )


def position_pattern(record):
    size = record["body_end"] - record["body_start"]
    fractions = [
        ((s["start"] + s["end"]) / 2 - record["body_start"]) / size
        for s in record["evidence_spans"]
    ]
    labels = ["front" if f < 1 / 3 else "middle" if f < 2 / 3 else "back" for f in fractions]
    return fractions, labels


def position_summary(run, workload, requests, counts, evidence, *, complete):
    if workload["purpose"] != "position":
        return None, []
    records = {r["case_id"]: r for r in workload["position_cases"]}
    output = Observations(run, workload["workload_id"], evidence, complete=complete)
    groups = defaultdict(list)
    variants = []
    for row in requests:
        record = records[row["case_id"]]
        actual = counts.get(row["case_id"], {}).get("actual_input_tokens")
        fractions, positions = position_pattern(record)
        groups[
            (record["family_id"], record["task_kind"], actual, tuple(positions), row["category"])
        ].append(row)
        variants.append(
            {
                "case_id": row["case_id"],
                "family_id": record["family_id"],
                "prompt_sha256": record["prompt_sha256"],
                "actual_input_tokens": actual,
                "position_fractions": fractions,
                "positions": positions,
            }
        )
    bins = []
    limits = [
        "variants_are_not_independent_questions",
        "position_is_character_midpoint_in_frozen_body",
        "no_long_context_comprehension_generalization",
        "comparison_gate_pending",
    ]
    for (family, task, actual, positions, category), rows in sorted(
        groups.items(), key=lambda item: str(item[0])
    ):
        valid = [r for r in rows if r["execution_state"] in ("completed", "failed")]
        passed = sum(
            r["execution_state"] == "completed"
            and (r.get("score") or {}).get("quality_state") == "pass"
            for r in valid
        )
        unscorable = any(
            r["execution_state"] == "completed"
            and (r.get("score") or {}).get("quality_state") not in ("pass", "fail")
            for r in valid
        )
        known = complete and actual is not None and bool(valid) and not unscorable
        value = passed / len(valid) if known else None
        bins.append(
            {
                "family_id": family,
                "task_kind": task,
                "actual_input_tokens": actual,
                "positions": list(positions),
                "category": category,
                "correct": passed,
                "valid_executed": len(valid),
                "excluded": len(rows) - len(valid),
                "value": value,
            }
        )
        output.add(
            "X02",
            "position_family_quality_rate",
            value,
            category=category,
            count=len(valid),
            numerator=passed if known else None,
            denominator=len(valid) if known else None,
            excluded=len(rows) - len(valid),
            reason="incomplete_unscorable_or_unknown_input_length",
            comparison=False,
            limits=limits,
            source=f"position_case_ledger:family={family}:kind={task}:tokens={actual}:positions={','.join(positions)}",
        )
    return {
        "family_count": len({r["family_id"] for r in records.values()}),
        "variant_count": len(records),
        "variants": variants,
        "bins": bins,
        "limitations": limits,
    }, output.items
