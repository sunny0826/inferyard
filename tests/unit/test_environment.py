from copy import deepcopy

import pytest

from inferyard.analysis.environment import FIELDS, assess_environment, assess_schedule
from inferyard.evidence.storage import EvidenceError
from tests.unit.test_cpu_policy import inventory


def state():
    return {
        **dict.fromkeys(FIELDS, "known"),
        "swap_pages": {"pswpin": 0, "pswpout": 0},
        "cpu_policies": inventory(),
    }


def test_return_to_initial_state_does_not_hide_interference():
    start = state()
    middle = {**start, "governor": "changed"}
    result = assess_environment(
        start,
        start,
        [
            {"monotonic_ns": 1, "snapshot": middle},
            {"monotonic_ns": 2, "snapshot": start},
        ],
        {},
        [],
    )
    assert result["reasons"] == ["environment_changed:governor"]
    assert not result["stable_observed_environment"]


@pytest.mark.parametrize(
    "kind,reason",
    [
        ("unknown", "environment_unknown:epp"),
        ("reset", "swap_counter_reset:pswpin"),
        ("swap", "system_swap_activity:pswpin"),
        ("frozen", "frozen_environment_mismatch:governor"),
    ],
)
def test_unknown_reset_swap_and_frozen_conditions(kind, reason):
    start = state()
    end = deepcopy(start)
    conditions = {}
    if kind == "unknown":
        end["epp"] = None
    elif kind == "reset":
        start["swap_pages"]["pswpin"] = 1
    elif kind == "swap":
        end["swap_pages"]["pswpin"] = 1
    else:
        conditions["governor"] = "required"
    result = assess_environment(
        start, end, [{"monotonic_ns": 1, "snapshot": start}], conditions, []
    )
    assert reason in result["reasons"]
    assert not result["comparison_eligible"]


def test_stable_environment_does_not_open_performance_gate():
    result = assess_environment(
        state(), state(), [{"monotonic_ns": 1, "snapshot": state()}], {}, []
    )
    assert result["stable_observed_environment"]
    assert not result["comparison_eligible"]


def test_timeline_gap_and_reversed_clock():
    observations = [{"monotonic_ns": 1, "snapshot": state()}]
    result = assess_environment(
        state(), state(), observations, {}, [{"t_send_ns": 0, "t_terminal_ns": 10_000_000_000}]
    )
    assert "environment_sampling_gap" in result["reasons"]
    with pytest.raises(EvidenceError, match="clock"):
        assess_environment(state(), state(), observations * 2, {}, [])


def test_schedule_cost_not_perturbation_gate():
    result = assess_schedule(
        [
            {
                "scheduled_ns": 0,
                "actual_ns": 10,
                "late_ns": 10,
                "collector_work_ns": 2_000_000,
                "queue_depth": 0,
            },
            {"kind": "resource_boundary", "read_started_ns": 20, "read_finished_ns": 40},
        ],
        1,
    )
    assert result["work_exceeds_interval_count"] == 1
    assert result["boundary_work_ns"] == 20
    assert result["overhead_gate"] == "not_verified"
    with pytest.raises(EvidenceError, match="schedule"):
        assess_schedule([{"collector_work_ns": -1}], 1)
