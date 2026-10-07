import hashlib
from copy import deepcopy

import pytest

from inferyard.analysis.position import (
    position_pattern,
    position_summary,
    validate_position_cases,
)
from inferyard.contracts.validation import ContractError, validate_document
from tests.unit.test_observations import EVIDENCE, RUN
from tests.unit.test_phase2_contracts import experiment


def digest(text):
    return hashlib.sha256(text.encode()).hexdigest()


def fixture():
    cases, records, rows = [], [], []
    for i, start in enumerate((5, 45, 85)):
        text = "填" * start + "证据甲" + "填" * (100 - start - 3)
        cid = f"case-{i}"
        cases.append(
            {
                "case_id": cid,
                "category": "qa",
                "prompt": text,
                "rules": {"answers": ["甲"], "normalization": "strip"},
            }
        )
        records.append(
            {
                "case_id": cid,
                "family_id": "family-1",
                "task_kind": "retrieval",
                "prompt_sha256": digest(text),
                "body_start": 0,
                "body_end": 100,
                "evidence_spans": [{"start": start, "end": start + 3, "sha256": digest("证据甲")}],
            }
        )
        rows.append(
            {
                "case_id": cid,
                "category": "qa",
                "request_id": f"r{i}",
                "execution_state": "failed" if i == 1 else "completed",
                "score": None if i == 1 else {"quality_state": "pass"},
            }
        )
    work = {
        "workload_id": "w",
        "purpose": "position",
        "protocol": {"kind": "fixed", "case_ids": [c["case_id"] for c in cases]},
        "position_cases": records,
    }
    return work, {"cases": cases}, rows


def test_frozen_positions_and_families_do_not_count_variants_as_questions():
    work, bundle, rows = fixture()
    validate_position_cases(work, bundle)
    assert [position_pattern(r)[1] for r in work["position_cases"]] == [
        ["front"],
        ["middle"],
        ["back"],
    ]
    counts = {row["case_id"]: {"actual_input_tokens": 128} for row in rows}
    summary, metrics = position_summary(RUN, work, rows, counts, EVIDENCE, complete=True)
    assert summary["family_count"] == 1 and summary["variant_count"] == 3
    assert sorted(row["value"] for row in summary["bins"]) == [0, 1, 1]
    assert all(m["denominator"] == 1 for m in metrics)
    assert all(not m["comparison_eligible"] for m in metrics)


@pytest.mark.parametrize("change", ["text", "span", "family_rules", "missing", "overlap"])
def test_metadata_mismatch_rejected(change):
    work, bundle, _ = fixture()
    if change == "text":
        bundle["cases"][0]["prompt"] += "changed"
    elif change == "span":
        work["position_cases"][0]["evidence_spans"][0]["sha256"] = "0" * 64
    elif change == "family_rules":
        bundle["cases"][1]["rules"]["answers"] = ["乙"]
    elif change == "missing":
        work["position_cases"].pop()
    else:
        record = work["position_cases"][0]
        record["task_kind"] = "dual_evidence"
        record["evidence_spans"].append(deepcopy(record["evidence_spans"][0]))
    with pytest.raises(ContractError):
        validate_position_cases(work, bundle)


def test_unknown_length_and_incomplete_never_produce_complete_quality():
    work, _, rows = fixture()
    for complete, counts in (
        (True, {}),
        (False, {r["case_id"]: {"actual_input_tokens": 128} for r in rows}),
    ):
        _, metrics = position_summary(RUN, work, rows, counts, EVIDENCE, complete=complete)
        assert all(m["value"] is None for m in metrics)


def test_position_metadata_is_serializable_in_frozen_experiment_contract():
    data = experiment()
    work, _, _ = fixture()
    data["workloads"][0].update(
        purpose="position", protocol=work["protocol"], position_cases=work["position_cases"]
    )
    validate_document("experiment", data)


@pytest.mark.parametrize("change", ["prefix", "suffix", "reference", "policy", "rules", "evidence"])
def test_cross_workload_family_requires_same_question_and_scoring(change):
    from inferyard.analysis.position import register_position_families

    work, bundle, _ = fixture()
    families = {}
    register_position_families(work, bundle, families)
    # Each variant may be in its own workload, so local validation alone passes.
    variant = deepcopy(work)
    variant["protocol"]["case_ids"] = ["case-0"]
    variant["position_cases"] = [deepcopy(work["position_cases"][0])]
    other = {"cases": [deepcopy(bundle["cases"][0])]}
    case, record = other["cases"][0], variant["position_cases"][0]
    if change in ("prefix", "suffix"):
        case["prompt"] = (
            "不同问题" + case["prompt"] if change == "prefix" else case["prompt"] + "不同问题"
        )
        if change == "prefix":
            record["body_start"] += 4
            record["body_end"] += 4
            for span in record["evidence_spans"]:
                span["start"] += 4
                span["end"] += 4
        record["prompt_sha256"] = digest(case["prompt"])
    elif change == "reference":
        case["reference_answer"] = "different"
    elif change == "policy":
        other["answer_policy"] = {"strip_line_edges": False}
    elif change == "rules":
        case["rules"]["answers"] = ["乙"]
    else:
        case["prompt"] = case["prompt"].replace("证据甲", "证据乙")
        record["prompt_sha256"] = digest(case["prompt"])
        record["evidence_spans"][0]["sha256"] = digest("证据乙")
    validate_position_cases(variant, other)
    with pytest.raises(ContractError, match="across workloads"):
        register_position_families(variant, other, families)
