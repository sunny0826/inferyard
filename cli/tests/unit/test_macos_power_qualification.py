from copy import deepcopy

import pytest

from inferyard.analysis.environment import assess_environment
from inferyard.analysis.environment_identity import MACOS_FIELDS, fields
from inferyard.config.environment_binding import mismatches
from inferyard.platforms.power_macos import assess, parse, valid

TEXT = "Battery Power:\n lowpowermode 1\nAC Power:\n lowpowermode 0\n"


def snapshots():
    power = parse(TEXT, True)
    environment = dict.fromkeys(MACOS_FIELDS, "known")
    environment.update(
        platform="Darwin",
        macos_power_policy=power,
        ac_online=True,
        profile="macos-low-power:0",
        governor=None,
        epp=None,
        scaling_driver=None,
        cpu_policies={"status": "unavailable"},
        swap_pages={"pswpin": 0, "pswpout": 0},
    )
    conditions = {
        "ac_online": True,
        "profile": environment["profile"],
        "governor": "unknown",
        "epp": "unknown",
        "allow_unknown_environment": True,
        "macos_power_policy": power,
    }
    return environment, conditions


def test_active_native_power_policy_does_not_invent_governor_or_high_power_support():
    power = parse(TEXT, True)
    assert (
        valid(power) and power["power_mode"] == "unsupported" and not power["power_mode_supported"]
    )
    assert parse(TEXT, False)["low_power_mode"] == 1
    environment, conditions = snapshots()
    assert mismatches(conditions, environment) == []
    assert assess([power, power], conditions)["complete_and_stable"]
    assert "governor" not in fields([environment], include_policy=True)
    assert "governor" in fields([{"platform": "Linux"}], include_policy=True)


@pytest.mark.parametrize(
    "text",
    [
        None,
        "AC Power:\n lowpowermode garbage",
        "AC Power:\n lowpowermode 0\n lowpowermode 1",
        "AC Power:\n lowpowermode 2",
    ],
)
def test_missing_or_ambiguous_policy_is_rejected(text):
    assert parse(text, True) is None


def test_platform_and_frozen_policy_changes_remain_qualification_failures():
    environment, conditions = snapshots()
    observations = [{"monotonic_ns": 1, "snapshot": environment}]
    assert assess_environment(environment, environment, observations, conditions, [])[
        "stable_observed_environment"
    ]
    changed = deepcopy(environment)
    changed["macos_power_policy"]["low_power_mode"] = 1
    result = assess_environment(environment, changed, observations, conditions, [])
    assert "macos_power_policy_changed" in result["reasons"]
    old = {k: v for k, v in conditions.items() if k != "macos_power_policy"}
    result = assess_environment(environment, environment, observations, old, [])
    assert "macos_power_policy_not_frozen" in result["reasons"]
    changed["platform"] = "Linux"
    assert (
        "environment_changed:platform"
        in assess_environment(environment, changed, observations, conditions, [])["reasons"]
    )
    assert "macos_power_policy" in mismatches(conditions, changed)


def test_config_rejects_contradictory_power_mode_support(config_path):
    from inferyard.config.loader import load_config
    from inferyard.contracts.validation import ContractError, Document

    config = load_config(config_path).config.to_dict()
    policy = parse(TEXT, True)
    config["conditions"]["macos_power_policy"] = policy
    Document.parse("config", config)
    policy["power_mode"] = 0
    with pytest.raises(ContractError, match="inconsistent native policy"):
        Document.parse("config", config)
