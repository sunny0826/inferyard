from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.output_budget import target_rate
from tests.unit.test_comparison import trial


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


def test_strict_and_natural_workloads_cannot_be_compared_as_same_protocol():
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
