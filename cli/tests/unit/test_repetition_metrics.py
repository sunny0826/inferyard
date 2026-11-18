from copy import deepcopy

import pytest

from inferyard.analysis.repetition_metrics import repetition_metrics
from inferyard.errors import EvidenceError


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
