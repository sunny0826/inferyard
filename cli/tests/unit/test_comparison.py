from copy import deepcopy

from inferyard.analysis.comparison import compare_trials


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


def test_model_and_locations_may_change_but_measurement_source_may_not():
    left, right = trial(), trial()
    right["config"]["model"]["sha256"] = "other-model"
    right["config"]["model"]["template_sha256"] = "other-template"
    right["config"]["engine"]["startup_args"][-1] = "5678"
    assert compare_trials(left, right)["eligibility"]["quality"]
    right["run"]["tool_source_sha256"] = "other-tool"
    assert not compare_trials(left, right)["eligibility"]["quality"]


def test_config_declared_factor_propagates_to_startup_but_second_factor_blocks():
    left, right = trial(), trial()
    for data in (left, right):
        data["plan"]["experiment"]["comparison"] = {"mode": "config", "factor": "threads"}
    right["config"]["conditions"]["threads"] = 8
    right["config"]["engine"]["startup_args"][1] = "8"
    assert compare_trials(left, right)["eligibility"]["quality"]
    right["config"]["conditions"]["threads_batch"] = 8
    assert not compare_trials(left, right)["eligibility"]["quality"]


def test_unknown_incomplete_and_side_by_side_never_produce_deltas():
    left = trial()
    for right, mode in ((trial(), "side-by-side"), (trial(), "config")):
        assert compare_trials(left, right, mode=mode)["completion_rate_difference"] is None
    for field in ("unknown", "incomplete"):
        right = trial()
        if field == "unknown":
            right["identity"]["verification"] = None
        else:
            right["summary"]["completeness"] = "incomplete"
        assert not compare_trials(left, right)["eligibility"]["completion"]
