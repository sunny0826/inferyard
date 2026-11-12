"""Frozen character spans bind position variants to a shared question family."""

import hashlib

from inferyard.contracts.validation import json_bytes
from inferyard.errors import ContractError


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
