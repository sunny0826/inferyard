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


def test_missing_tokens_never_become_zero_or_target_failure():
    for metric in build(missing=True):
        if metric["statistic"] in ("actual_output_tokens", "observed_target_reached"):
            assert metric["value"] is None and metric["status"] == "missing"
