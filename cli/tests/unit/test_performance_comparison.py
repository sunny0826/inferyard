import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.environment import FIELDS


def trial():
    generation = dict.fromkeys(
        (
            "seed",
            "temperature",
            "top_k",
            "top_p",
            "min_p",
            "presence_penalty",
            "repeat_penalty",
            "max_tokens",
        ),
        1,
    )
    return {
        "run": {
            "origin": "measured",
            "trial_id": "t",
            "definition_versions": {"measurement": "v2", "scoring": "phase2.v1"},
            "run_id": "synthetic",
            "tool_source_sha256": "a",
            "tool_version": "1",
        },
        "bundle": {"cases": [{"case_id": "case"}], "answer_policy": {}, "task_protocol": "quality"},
        "selection": {"bundle_sha256": "b", "scorer_sha256": "c", "case_ids": ["case"]},
        "config": {
            "device": {"id": "host"},
            "generation": generation,
            "execution": {"timeout": 10},
            "conditions": {"threads": 4, "threads_batch": 4},
            "telemetry": {"interval_ms": 500},
            "engine": {
                "adapter": "prism",
                "release": "1",
                "binary_sha256": "d",
                "backend": "cpu",
                "startup_args": ["-t", "4", "--port", "1234"],
            },
            "model": dict.fromkeys(
                ("repo", "revision", "sha256", "packing", "template_sha256"), "m"
            ),
        },
        "identity": {
            "verification": "verified",
            "runtime_library_hashes": {"lib": "x"},
            "effective_parameters": {
                "parameters": {
                    k: {"verification": "verified", "effective": v} for k, v in generation.items()
                }
            },
        },
        "environment_start": dict.fromkeys(
            ("platform", "architecture", "cpu_model", "kernel"), "known"
        ),
        "summary": {
            "completeness": "complete",
            "completion_rate": {"value": 1},
            "counts": {"planned": 1, "valid_executed": 1},
        },
        "plan": {
            "experiment": {
                "comparison": {"mode": "model", "factor": None},
                "workloads": [
                    {
                        "workload_id": "w",
                        "purpose": "quality",
                        "protocol": {"kind": "fixed", "case_ids": ["case"]},
                        "input_target_tokens": None,
                        "output_budget_tokens": 1,
                        "timeout_seconds": 10,
                    }
                ],
            },
            "trials": [{"trial_id": "t", "workload_id": "w"}],
        },
    }


def inputs():
    left, right = trial(), trial()
    proofs = []
    for index, data in enumerate((left, right)):
        data["run"]["run_id"] = str(index)
        data["environment_start"].update(dict.fromkeys((*FIELDS, "cpu_policies"), "known"))
        data["plan"]["experiment"]["performance_environment"] = {
            "max_external_cpu_percent": 25,
            "max_external_interval_seconds": 1,
        }
        data["summary"]["measurement_context"] = {"environment_qualification": {"eligible": True}}
        data["requests"] = [
            {
                "request_id": "r" + str(index),
                "case_id": "case",
                "category": "qa",
                "execution_state": "completed",
                "token_source": "endpoint.usage",
                "token_scope": "completion_tokens",
            }
        ]
        data["summary"]["metric_observations"] = [
            {
                "metric_id": code,
                "statistic": "p50",
                "group": {"category": "qa"},
                "request_id": None,
                "source": "same",
                "layer": "client",
                "definition_version": "v2",
                "unit": "token/s" if code == "L04" else "ms",
                "value": 100 + 20 * index,
                "missing_reason": None,
                "sample_count": 1,
                "excluded": 0,
                "comparison_eligible": False,
                "limitations": ["performance_comparison_gate_pending"],
            }
            for code in ("L01", "L03", "L04")
        ]
        proofs.append(
            {
                "eligible": True,
                "reasons": [],
                "target_run_id": str(index),
                "tolerance_ratio": 0.05,
                "total_observer_binding": {"eligible": True, "evidence_kind": "unit_fixture"},
            }
        )
    return left, right, proofs


@pytest.mark.parametrize(
    "change", ["overhead", "binding", "environment", "incomplete", "side_by_side"]
)
def test_failed_prerequisite_cannot_produce_any_performance_delta(change):
    left, right, proofs = inputs()
    mode = None
    if change == "overhead":
        proofs[1].update(eligible=False, reasons=["not_before_target"])
    elif change == "binding":
        proofs[1]["target_run_id"] = "wrong"
    elif change == "environment":
        right["environment_start"]["cpu_policies"] = "different"
    elif change == "incomplete":
        right["summary"]["completeness"] = "incomplete"
    else:
        mode = "side-by-side"
    result = compare_trials(left, right, mode=mode, performance_evidence=proofs)
    assert not result["eligibility"]["performance"]
    assert result["performance_analysis"]["blockers"]
    assert all(r["difference"] is None for r in result["performance_analysis"]["differences"])


def test_different_model_tokenizer_blocks_rate_but_not_task_e2e():
    left, right, proofs = inputs()
    right["config"]["model"]["sha256"] = "different-model"
    rows = {
        r["metric_id"]: r
        for r in compare_trials(left, right, performance_evidence=proofs)["performance_analysis"][
            "differences"
        ]
    }
    assert rows["L03"]["eligible"]
    assert rows["L04"]["difference"] is None
    assert "tokenizer_identity_not_proven_equal" in rows["L04"]["reasons"]
