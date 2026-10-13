from copy import deepcopy

import pytest

from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import ContractError
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.fixed_output import target_rate, validate_inputs, verify_probe
from tests.unit.test_phase2_contracts import experiment


def response(count=16, state="completed", finish="length"):
    return dict(
        execution_state=state,
        completion_tokens=count,
        raw_finish_reason=finish,
        token_source="endpoint.usage",
        token_scope="completion_tokens",
    )


def test_strict_rate_retains_failed_denominator_and_does_not_use_finish_alone():
    rows = [response(), response(8, finish="stop"), response(16, state="failed")]
    r = target_rate(rows, 16, True)
    assert r["value"] == 1 / 3 and r["denominator"] == 3
    assert target_rate([response(8)], 16, True)["value"] == 0
    assert target_rate([response(17)], 16, True)["value"] == 0
    assert target_rate(rows, 16, False)["value"] is None
    rows.append(response(None, state="failed"))
    r = target_rate(rows, 16, True)
    assert r["value"] is None and r["denominator"] == 4 and r["numerator"] is None
    assert target_rate([dict(execution_state="not_executed")], 16, True)["excluded"] == 1


@pytest.mark.parametrize("change", ["flag", "count", "finish", "scope", "missing", "budget"])
def test_probe_rejects_unverified_parameters_or_counts(change):
    effective = {"slots": [{"params": {"ignore_eos": True, "max_tokens": 16}}]}
    r = response()
    assert verify_probe(effective, r, 16)["ignore_eos"] is True
    if change == "flag":
        effective["slots"][0]["params"]["ignore_eos"] = 1
    elif change == "budget":
        effective["slots"][0]["params"]["max_tokens"] = 17
    elif change == "count":
        r["completion_tokens"] = 15
    elif change == "finish":
        r["raw_finish_reason"] = "stop"
    elif change == "scope":
        r["token_scope"] = "visible"
    else:
        effective.clear()
    with pytest.raises(PreflightError):
        verify_probe(effective, r, 16)


def test_strict_mode_freezes_separately_and_cannot_score_quality():
    source = experiment()
    w = source["workloads"][0]
    w["output_mode"] = "strict_fixed_length"
    with pytest.raises(ContractError):
        compile_plan(source)
    w["purpose"] = "performance"
    strict = compile_plan(source)
    old = deepcopy(source)
    old["workloads"][0].pop("output_mode")
    assert strict["plan_sha256"] != compile_plan(old)["plan_sha256"]
    config = {"generation": {"stop": []}}
    bundle = {
        "cases": [
            {"case_id": "c1", "category": "performance", "rules": {"output_target_tokens": 512}}
        ]
    }
    validate_inputs(w, config, bundle)
    for change in ("quality", "stop", "target"):
        c, b = deepcopy(config), deepcopy(bundle)
        if change == "quality":
            b["cases"][0]["category"] = "qa"
        elif change == "stop":
            c["generation"]["stop"] = ["end"]
        else:
            b["cases"][0]["rules"]["output_target_tokens"] = 1
        with pytest.raises(ContractError):
            validate_inputs(w, c, b)


def test_strict_and_natural_workloads_cannot_be_compared_as_same_protocol():
    from inferyard.analysis.comparison import compare_trials
    from tests.unit.test_comparison import trial

    left, right = trial(), trial()
    for d in (left, right):
        d["plan"]["experiment"]["workloads"][0]["purpose"] = "performance"
    right["plan"]["experiment"]["workloads"][0]["output_mode"] = "strict_fixed_length"
    result = compare_trials(left, right)
    assert not any(result["eligibility"].values())
    assert (
        next(c for c in result["conditions"] if c["field"] == "task_protocol")["status"]
        == "different"
    )
