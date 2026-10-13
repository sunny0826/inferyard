import pytest

from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import ContractError
from tests.unit.test_phase2_contracts import experiment


def test_capacity_policy_requires_frozen_performance_length_protocol():
    data = experiment()
    data["capacity_stop"] = "first_failed_request"
    with pytest.raises(ContractError, match="fixed performance length"):
        compile_plan(data)
    workload = data["workloads"][0]
    workload.update(purpose="performance", input_target_tokens=256)
    plan = compile_plan(data)
    assert plan["experiment"]["capacity_stop"] == "first_failed_request"
    data.pop("capacity_stop")
    assert compile_plan(data)["plan_sha256"] != plan["plan_sha256"]


def test_replay_rejects_work_after_first_failure_and_false_stop_claim():
    from inferyard.evidence.capacity_stop import validate_capacity_stop
    from inferyard.evidence.storage import EvidenceError

    policy = {"capacity_stop": "first_failed_request"}
    failed = {"execution_state": "failed", "request_id": "a"}
    pending = {"execution_state": "not_executed", "request_id": None}
    validate_capacity_stop(policy, [failed, pending], "capacity_scan_failed_request")
    for rows, reason in (
        ([failed], "plan_finished"),
        (
            [failed, {"execution_state": "completed", "request_id": "b"}],
            "capacity_scan_failed_request",
        ),
        ([pending], "capacity_scan_failed_request"),
    ):
        with pytest.raises(EvidenceError, match="capacity_stop"):
            validate_capacity_stop(policy, rows, reason)
