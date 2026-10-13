from copy import deepcopy

import pytest

from inferyard.analysis.environment import measurement_context
from inferyard.evidence.storage import json_bytes
from tests.unit.test_cpu_policy import inventory
from tests.unit.test_environment import state
from tests.unit.test_external_cpu import ENDPOINT, rows


def context(tmp_path, change=None, *, late_ns=0):
    snapshot = state()
    snapshot.update(governor="performance", epp="performance", cpu_policies=inventory())
    ending = deepcopy(snapshot)
    external = rows()
    request = {
        "request_id": "r",
        "execution_state": "completed",
        "t_send_ns": 100_000_000,
        "t_terminal_ns": 900_000_000,
    }
    policy = {"max_external_cpu_percent": 25, "max_external_interval_seconds": 1}
    schedule = {
        "scheduled_ns": 0,
        "actual_ns": late_ns,
        "late_ns": late_ns,
        "collector_work_ns": 1000,
        "queue_depth": 0,
    }
    if change == "cpu":
        ending["cpu_policies"]["policies"][0]["scaling_governor"] = "powersave"
    elif change == "threshold":
        policy["max_external_cpu_percent"] = 10
    elif change == "coverage":
        external = external[:1]
    elif change == "unfrozen":
        policy = None
    elif change == "incomplete":
        request["execution_state"] = "invalid"
    elif change == "schedule":
        schedule["collector_work_ns"] = 2_000_000_000
    files = {
        "environment.start.json": json_bytes(snapshot),
        "environment.end.json": json_bytes(ending),
        "environment.jsonl": json_bytes({"monotonic_ns": 500_000_000, "snapshot": snapshot}),
        "schedule.jsonl": json_bytes(schedule),
        "external-cpu.jsonl": b"".join(json_bytes(r) for r in external),
    }
    for name, raw in files.items():
        (tmp_path / name).write_bytes(raw)
    return measurement_context(
        tmp_path,
        files,
        {
            "conditions": {"governor": "performance", "epp": "performance"},
            "endpoint": ENDPOINT,
            "telemetry": {"interval_ms": 1000},
        },
        [request],
        performance_policy=policy,
    )


def test_complete_evidence_qualifies_environment_but_not_performance(tmp_path):
    result = context(tmp_path)
    assert result["environment_qualification"]["eligible"]
    assert result["environment_qualification"]["reasons"] == []
    assert not result["performance_comparison_eligible"]
    assert result["collector_schedule"]["overhead_gate"] == "not_verified"


@pytest.mark.parametrize(
    "change", ["cpu", "threshold", "coverage", "unfrozen", "incomplete", "schedule"]
)
def test_each_incomplete_or_conflicting_prerequisite_blocks_environment(tmp_path, change):
    result = context(tmp_path, change)
    assert not result["environment_qualification"]["eligible"]
    assert result["environment_qualification"]["reasons"]
    assert not result["performance_comparison_eligible"]


@pytest.mark.parametrize("late_ns", [2_000_000_000, 2_000_000_001])
def test_schedule_lateness_limit_is_two_declared_intervals(tmp_path, late_ns):
    result = context(tmp_path, late_ns=late_ns)
    qualification = result["environment_qualification"]
    assert qualification["eligible"] == (late_ns == 2_000_000_000)
    assert qualification["reasons"] == (
        [] if late_ns == 2_000_000_000 else ["collector_schedule_lateness_unknown_or_excessive"]
    )
    assert result["collector_schedule"]["max_late_ns"] == late_ns
    assert not result["performance_comparison_eligible"]
