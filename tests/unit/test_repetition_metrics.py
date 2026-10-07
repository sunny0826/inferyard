from copy import deepcopy

import pytest

from inferyard.analysis.repetition_metrics import repetition_metrics
from inferyard.evidence.storage import EvidenceError


def inputs():
    ids = [f"c{i}" for i in range(40)]
    plan = {
        "plan_sha256": "p",
        "trials": [{"trial_id": f"t{i}", "workload_id": "w", "case_order": ids} for i in range(3)],
        "experiment": {
            "workloads": [
                {"workload_id": "w", "repeats": 3, "protocol": {"kind": "fixed", "case_ids": ids}}
            ]
        },
    }
    runs = [
        {
            "run": {
                "run_id": f"r{i}",
                "trial_id": f"t{i}",
                "plan_sha256": "p",
                "relation": "initial",
            },
            "summary": {"completeness": "complete"},
            "requests": [
                {
                    "case_id": cid,
                    "category": "qa",
                    "quality_state": "fail" if i == 1 and j >= 30 else "pass",
                    "execution_state": "completed",
                    "t_send_ns": 0,
                    "t_terminal_ns": (i + 1) * 1_000_000,
                }
                for j, cid in enumerate(ids)
            ],
        }
        for i in range(3)
    ]
    return plan, runs


def test_all_k_passes_use_unique_case_denominator():
    plan, runs = inputs()
    group = repetition_metrics(plan, runs)[0]
    assert group["S03"] == {"numerator": 30, "denominator": 40, "value": 0.75, "reason": None}
    assert group["S04"]["trial_medians_ms"] == [1, 2, 3]
    assert group["S04"]["range_ms"] == 2
    assert group["S04"]["iqr_ms"] == 2
    assert group["cases"][0]["paired_latency_difference_ms"] == [0, 1, 2]
    assert not group["S04"]["comparison_eligible"]


@pytest.mark.parametrize("fault", ["missing", "resume", "partial", "unscorable"])
def test_fragments_and_missing_repeats_never_fill_full_result(fault):
    plan, runs = inputs()
    if fault == "missing":
        runs.pop()
    elif fault == "resume":
        runs[2]["run"]["relation"] = "resume"
    elif fault == "partial":
        runs[2]["summary"]["completeness"] = "incomplete"
    else:
        runs[2]["requests"][0]["quality_state"] = "unscorable"
    result = repetition_metrics(plan, runs)[0]
    assert result["S03"]["value"] is None
    assert result["S03"]["denominator"] == 40


def test_failed_performance_case_prevents_composition_change():
    plan, runs = inputs()
    runs[1]["requests"][0]["execution_state"] = "failed"
    result = repetition_metrics(plan, runs)[0]
    assert result["S04"]["trial_medians_ms"] == [1, None, 3]
    assert result["S04"]["range_ms"] is None
    assert result["cases"][0]["paired_latency_difference_ms"][1] is None


def test_foreign_or_duplicate_run_rejected_and_ambiguous_repeat_missing():
    plan, runs = inputs()
    with pytest.raises(EvidenceError):
        repetition_metrics(plan, runs + [runs[0]])
    duplicate = deepcopy(runs[0])
    duplicate["run"]["run_id"] = "new"
    assert repetition_metrics(plan, runs + [duplicate])[0]["S03"]["value"] is None
    runs[0]["run"]["plan_sha256"] = "other"
    with pytest.raises(EvidenceError):
        repetition_metrics(plan, runs)
