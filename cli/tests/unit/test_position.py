import hashlib
from copy import deepcopy

import pytest

from inferyard.analysis.position import position_pattern, position_summary
from inferyard.contracts.position_cases import validate_position_cases
from inferyard.errors import ContractError
from tests.unit.test_observations import EVIDENCE, RUN


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
