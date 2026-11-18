"""EPP opt-out applies to execution, while observations and comparison limits remain."""

import json
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.analysis.environment import assess_environment
from inferyard.config.loader import load_config, validate_runtime_config
from inferyard.errors import ContractError
from inferyard.platforms import identity
from inferyard.platforms.cpu_policy import assess
from inferyard.runtime.safety import SafetyGuard
from tests.unit.test_cpu_policy import inventory
from tests.unit.test_environment import state
from tests.unit.test_safety import Sensors


def test_runtime_epp_change_does_not_stop_but_ac_condition_still_does():
    conditions = dict(
        ac_online=True,
        governor="powersave",
        profile="balanced",
        epp="balance_performance",
        require_epp_match=False,
    )
    observed = dict(conditions, epp="balance_power")
    policy = dict(
        check_environment=True,
        max_temperature_celsius=None,
        require_temperature=False,
        max_external_cpu_percent=None,
    )
    guard = SafetyGuard(policy, {"conditions": conditions}, lambda: observed, sensors=Sensors(99))
    guard.check(periodic=True)
    assert guard.last["environment"]["epp"] == "balance_power"
    observed["epp"] = None
    guard.check(periodic=True)
    assert guard.last["environment"]["epp"] is None
    observed["ac_online"] = False
    with pytest.raises(identity.PreflightError, match="environment_safety_condition_changed"):
        guard.check(periodic=True)
