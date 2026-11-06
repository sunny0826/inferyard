import pytest

from inferyard.config.single_plan import local_safety
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.local_profile import safety_policy

SAFETY = {
    "interval_seconds": 1,
    "max_temperature_celsius": 85,
    "require_temperature": True,
    "check_environment": True,
}


def plan(policy):
    workload = {"protocol": {"kind": "fixed"}, "input_target_tokens": None}
    return {"experiment": {"safety": policy, "workloads": [workload]}}


def test_override_freezes_null_threshold_but_keeps_temperature_required():
    config = {"safety": {**SAFETY, "temperature_stop_override_reason": "operator approved"}}
    policy = local_safety(config["safety"])
    assert policy["max_temperature_celsius"] is None
    assert policy["require_temperature"] is True
    assert "temperature_stop_override_reason" not in policy
    assert safety_policy(plan(policy), config) == policy


def test_null_threshold_without_declared_override_is_rejected():
    config = {"safety": dict(SAFETY)}
    policy = {**local_safety(SAFETY), "max_temperature_celsius": None}
    with pytest.raises(PreflightError, match="safety_policy_required"):
        safety_policy(plan(policy), config)


def test_declared_override_requires_the_frozen_null_threshold():
    config = {"safety": {**SAFETY, "temperature_stop_override_reason": "operator approved"}}
    with pytest.raises(PreflightError, match="safety_policy_required"):
        safety_policy(plan(local_safety(SAFETY)), config)
