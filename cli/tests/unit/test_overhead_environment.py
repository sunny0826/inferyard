from copy import deepcopy

import pytest

from inferyard.analysis.overhead_environment import assess_environments
from tests.unit.test_cpu_policy import inventory
from tests.unit.test_environment import state


def record():
    snapshot = state()
    snapshot["cpu_policies"] = inventory()
    return {
        "run_id": "r",
        "endpoints": [snapshot, deepcopy(snapshot)],
        "endpoint_reasons": [],
        "evidence_refs": [],
        "qualification": {"eligible": True, "reasons": []},
    }


def test_equal_endpoints_require_every_arm_window_qualification():
    arms = [record() for _ in range(4)]
    assert assess_environments(arms, record())["eligible"]
    arms[0]["qualification"] = {"eligible": False, "reasons": ["environment_timeline_missing"]}
    result = assess_environments(arms, record())
    assert not result["eligible"]
    assert "arm_1:environment_timeline_missing" in result["reasons"]
    assert result["runs"][1]["eligible"]


@pytest.mark.parametrize("field", ["cpu_model", "kernel", "cpu_policies", "ac_online"])
def test_stable_individual_runs_with_different_environment_cannot_match(field):
    arms = [record() for _ in range(4)]
    target = record()
    for endpoint in target["endpoints"]:
        endpoint[field] = "different"
    result = assess_environments(arms, target)
    assert not result["eligible"]
    assert "target:environment_identity_changed:" + field in result["reasons"]


def test_missing_identity_endpoints_or_arm_is_not_agreement():
    target = record()
    target["endpoints"][0].pop("cpu_model")
    target["endpoint_reasons"] = ["environment_endpoint_unsealed_or_missing:environment.start.json"]
    result = assess_environments([record()] * 3, target)
    assert not result["eligible"]
    assert "four_arm_environments_required" in result["reasons"]
    assert "target:environment_identity_unknown:cpu_model" in result["reasons"]
