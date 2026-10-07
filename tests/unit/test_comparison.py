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
            "definition_versions": {"measurement": "v2"},
            "tool_source_sha256": "a",
        },
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
        "summary": {"completeness": "complete", "completion_rate": {"value": 1}},
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


def test_performance_change_does_not_erase_quality_and_never_grants_performance():
    left, right = trial(), trial()
    left["config"]["conditions"]["profile"] = "balanced"
    right["config"]["conditions"]["profile"] = "performance"
    result = compare_trials(left, right)
    assert result["eligibility"]["quality"]
    assert not result["eligibility"]["performance"]
    assert any(
        c["field"] == "config.conditions.profile" and c["status"] == "different"
        for c in result["conditions"]
    )


def test_quality_difference_has_same_definition_and_explicit_direction():
    left, right = trial(), trial()
    metric = {
        "comparison_eligible": True,
        "metric_id": "Q01",
        "statistic": "pass_rate",
        "group": {"category": "qa"},
        "request_id": None,
        "definition_version": "v2",
        "unit": "ratio",
        "source": "scores",
        "layer": "experiment",
        "value": 0.5,
    }
    left["summary"]["metric_observations"] = [metric]
    right["summary"]["metric_observations"] = [{**deepcopy(metric), "value": 0.75}]
    result = compare_trials(left, right)
    assert result["quality_differences"][0]["difference"] == 0.25
    right["summary"]["metric_observations"][0]["definition_version"] = "other"
    assert compare_trials(left, right)["quality_differences"][0]["difference"] is None


def test_quality_metric_eligibility_cannot_be_overridden_by_pair_gate():
    left, right = trial(), trial()
    metric = {
        "metric_id": "Q01",
        "statistic": "pass_rate",
        "group": {"category": "qa"},
        "request_id": None,
        "definition_version": "v2",
        "unit": "ratio",
        "source": "scores",
        "layer": "experiment",
        "value": 0.5,
        "comparison_eligible": False,
    }
    left["summary"]["metric_observations"] = [metric]
    right["summary"]["metric_observations"] = [{**metric, "value": 0.75}]
    result = compare_trials(left, right)
    assert result["eligibility"]["quality"]
    assert result["quality_differences"][0]["difference"] is None


def test_safety_policy_must_match_including_disabled_thresholds():
    left, right = trial(), trial()
    policy = {"max_temperature_celsius": None, "interval_seconds": 1}
    left["plan"]["experiment"]["safety"] = policy
    right["plan"]["experiment"]["safety"] = dict(policy)
    assert "safety_policy:unknown" not in compare_trials(left, right)["blockers"]
    right["plan"]["experiment"]["safety"]["interval_seconds"] = 2
    assert "safety_policy:different" in compare_trials(left, right)["blockers"]


def test_cache_protocol_difference_blocks_controlled_comparison():
    left, right = trial(), trial()
    right["plan"]["experiment"]["workloads"][0]["cache_protocol"] = "prism_prefix_reuse.v1"
    assert "task_protocol:different" in compare_trials(left, right)["blockers"]
