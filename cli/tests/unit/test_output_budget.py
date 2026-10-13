from inferyard.analysis.output_budget import output_budget_observations
from tests.unit.test_observations import EVIDENCE, RUN


def build(*, complete=True, kind="fixed", missing=False):
    case = {
        "case_id": "performance-1",
        "category": "performance",
        "rules": {"output_target_tokens": 16},
    }
    rows = [
        {
            "case_id": case["case_id"],
            "category": "performance",
            "request_id": "r1",
            "execution_state": "completed",
            "raw_finish_reason": "stop",
            "budget_exhausted": False,
            "completion_tokens": None if missing else 8,
            "token_source": "endpoint.usage",
            "token_scope": "completion_tokens",
        },
        {
            "case_id": case["case_id"],
            "category": "performance",
            "request_id": "r2",
            "execution_state": "failed",
            "raw_finish_reason": None,
            "budget_exhausted": False,
            "completion_tokens": None,
            "token_source": None,
            "token_scope": None,
        },
    ]
    workload = {"workload_id": "w", "protocol": {"kind": kind}, "output_budget_tokens": 32}
    return output_budget_observations(RUN, workload, [case], rows, EVIDENCE, complete=complete)


def test_natural_stop_is_not_failure_or_fixed_length_claim():
    items = build()
    actual = next(
        m for m in items if m["statistic"] == "actual_output_tokens" and m["request_id"] == "r1"
    )
    assert actual["value"] == 8
    target = next(
        m for m in items if m["statistic"] == "observed_target_reached" and m["request_id"] == "r1"
    )
    assert target["value"] == 0
    stop = next(m for m in items if m["source"] == "length_case_ledger:finish=stop")
    assert stop["value"] == 0.5 and stop["denominator"] == 2
    unknown = next(m for m in items if m["source"] == "length_case_ledger:finish=unreported")
    assert unknown["numerator"] == 1
    fixed = next(m for m in items if m["statistic"] == "strict_fixed_length_target_rate")
    assert fixed["value"] is None and fixed["status"] == "not_applicable"
    assert not any(m["comparison_eligible"] for m in items)


def test_missing_tokens_never_become_zero_or_target_failure():
    for metric in build(missing=True):
        if metric["statistic"] in ("actual_output_tokens", "observed_target_reached"):
            assert metric["value"] is None and metric["status"] == "missing"


def test_incomplete_repeated_probes_are_explicitly_limited():
    items = build(complete=False, kind="duration")
    assert all("incomplete_trial_subset_only" in m["limitations"] for m in items)
    assert all("repeated_probes_not_independent_cases" in m["limitations"] for m in items)


def test_not_executed_and_unknown_terminal_state_remain_valid_missing_observations():
    case = {"case_id": "c", "category": "performance", "rules": {"output_target_tokens": None}}
    workload = {"workload_id": "w", "protocol": {"kind": "fixed"}, "output_budget_tokens": 32}
    for state, request in (("not_executed", None), ("failed", "r")):
        rows = [
            {
                "case_id": "c",
                "category": "performance",
                "request_id": request,
                "execution_state": state,
            }
        ]
        items = output_budget_observations(RUN, workload, [case], rows, EVIDENCE, complete=False)
        budget = next(m for m in items if m["statistic"] == "output_budget_exhaustion_rate")
        assert budget["value"] is None
