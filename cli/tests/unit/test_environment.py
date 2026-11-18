from copy import deepcopy

import pytest

from inferyard.analysis.environment import FIELDS, assess_environment, assess_schedule
from inferyard.errors import EvidenceError
from tests.unit.test_cpu_policy import inventory


def state():
    return {
        **dict.fromkeys(FIELDS, "known"),
        "swap_pages": {"pswpin": 0, "pswpout": 0},
        "cpu_policies": inventory(),
    }


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
