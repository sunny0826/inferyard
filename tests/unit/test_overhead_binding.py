import copy

import pytest

from inferyard.evidence.storage import sha256_file
from inferyard.runtime.overhead_binding import bind_target, workload_identity
from inferyard.runtime.overhead_runner import request_projection


@pytest.fixture
def evidence(tmp_path, monkeypatch):
    # Isolate applicability rules; integration tests replay schema-validated plans.
    monkeypatch.setattr(
        "inferyard.runtime.overhead_binding.trial_for",
        lambda plan, trial_id: next(t for t in plan["trials"] if t["trial_id"] == trial_id),
    )
    for name in ("manifest.json", "config.frozen.json", "bundle.json"):
        (tmp_path / name).write_text("{}")
    data = {
        "run": {"trial_id": "t1", "run_id": "r1", "tool_source_sha256": "a" * 64},
        "plan": {
            "trials": [{"trial_id": "t1", "workload_id": "w1"}],
            "experiment": {
                "definition_versions": {"scorer": "v1"},
                "workloads": [{"workload_id": "w1", "protocol": {"kind": "fixed"}}],
            },
        },
        "summary": {"stop_reason": "plan_finished"},
        "requests": [
            {
                "case_id": "c1",
                "execution_state": "completed",
                "content": "yes",
                "reasoning": "",
                "completion_tokens": 1,
                "token_source": "engine",
                "token_scope": "completion",
                "t_send_ns": 1,
                "t_terminal_ns": 2,
            }
        ],
    }
    protocol = {
        "case_ids": ["c1"],
        "tool_source_sha256": "a" * 64,
        "protocol_sha256": "b" * 64,
        "config_sha256": sha256_file(tmp_path / "config.frozen.json"),
        "bundle_sha256": sha256_file(tmp_path / "bundle.json"),
    }
    return tmp_path, data, protocol


def test_exact_binding_and_workload_changes(evidence):
    root, source, protocol = evidence
    identity = workload_identity(source)
    rows = [request_projection(r) for r in source["requests"]]

    def check(target):
        return bind_target(
            root,
            target,
            protocol,
            identity,
            rows,
            {"passed": True},
            {"collector": "linux-resource.v2"},
        )

    result = check(source)
    assert result["applicable"]
    assert not result["performance_comparison_eligible"]
    variants = [
        ("tool", "target_tool_mismatch"),
        ("output", "target_output_work_differs"),
        ("order", "target_case_order_mismatch"),
        ("workload", "target_workload_mismatch"),
        ("incomplete", "target_execution_incomplete"),
    ]
    for change, reason in variants:
        data = copy.deepcopy(source)
        if change == "tool":
            data["run"]["tool_source_sha256"] = "c" * 64
        elif change == "output":
            data["requests"][0]["content"] = "no"
        elif change == "order":
            data["requests"][0]["case_id"] = "c2"
        elif change == "workload":
            data["plan"]["experiment"]["workloads"][0]["protocol"]["kind"] = "duration"
        else:
            data["summary"]["stop_reason"] = "cancelled"
        result = check(data)
        assert not result["applicable"]
        assert reason in result["reasons"]
    (root / "config.frozen.json").write_text('{"changed":true}')
    assert "target_config_mismatch" in check(source)["reasons"]


def test_shared_safety_policy_changes_reject_target_binding(evidence):
    root, source, protocol = evidence
    source["plan"]["experiment"]["safety"] = {"interval_seconds": 1}
    identity = workload_identity(source)
    target = copy.deepcopy(source)
    target["plan"]["experiment"]["safety"] = {"interval_seconds": 5}
    result = bind_target(
        root,
        target,
        protocol,
        identity,
        [request_projection(r) for r in source["requests"]],
        {"passed": True},
        {"collector": "linux-resource.v2"},
    )
    assert not result["applicable"]
    assert "target_workload_mismatch" in result["reasons"]
