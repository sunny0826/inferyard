"""EPP opt-out applies to execution, while observations and comparison limits remain."""

import json
from copy import deepcopy
from types import SimpleNamespace

import pytest

from inferyard.analysis.environment import assess_environment
from inferyard.config.loader import load_config, validate_runtime_config
from inferyard.contracts.validation import ContractError
from inferyard.platforms import identity
from inferyard.platforms.cpu_policy import assess
from inferyard.runtime.safety import SafetyGuard
from tests.unit.test_cpu_policy import inventory
from tests.unit.test_environment import state
from tests.unit.test_safety import Sensors


@pytest.mark.parametrize("observed", ["balance_power", None])
@pytest.mark.parametrize("required", [None, True, False])
def test_preflight_epp_opt_out_is_explicit_and_keeps_observation(
    observed, required, config_path, tmp_path, monkeypatch
):
    monkeypatch.setattr(identity.platform, "system", lambda: "Linux")
    config = load_config(config_path).config.to_dict()
    config["conditions"].update(
        ac_online=True, governor="powersave", profile="balanced", epp="balance_performance"
    )
    if required is not None:
        config["conditions"]["require_epp_match"] = required
    validate_runtime_config(config)
    manifest = tmp_path / "libraries.json"
    manifest.write_text(json.dumps({"library.so": "a" * 64}))
    config["engine"]["runtime_library_manifest"] = str(manifest)
    config["output"]["root"] = str(tmp_path)
    environment = dict(config["conditions"], epp=observed, mem_available_bytes=16 * 1024**3)
    monkeypatch.setattr(
        identity, "hash_file", lambda *a: SimpleNamespace(size=1024**3, unchanged=lambda: True)
    )
    monkeypatch.setattr(
        "inferyard.platforms.device_preflight.nvidia_snapshot",
        lambda: {"status": "unavailable", "devices": []},
    )
    monkeypatch.setattr(identity, "verify_process", lambda *a, **k: {"verification": "verified"})
    monkeypatch.setattr(identity, "environment_snapshot", lambda: environment)
    monkeypatch.setattr(
        identity.shutil, "disk_usage", lambda *a: SimpleNamespace(free=16 * 1024**3)
    )
    if required is False:
        result, _ = identity.static_preflight(config)
        assert result["environment"]["epp"] == observed
    else:
        with pytest.raises(identity.PreflightError, match="frozen_environment_mismatch"):
            identity.static_preflight(config)

    diagnostic, _ = identity.static_preflight(config, diagnostic=True)
    assert diagnostic["environment_admission"]["blockers"] == []
    assert "epp" in diagnostic["environment_admission"]["differences"]
    observed_only = {"definition": "environment-admission.v2", "required_fields": []}
    assert identity.static_preflight(config, environment_policy=observed_only)
    required_epp = {**observed_only, "required_fields": ["epp"]}
    with pytest.raises(identity.PreflightError, match="frozen_environment_mismatch"):
        identity.static_preflight(config, environment_policy=required_epp)


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


def test_reporting_ignores_only_frozen_epp_match_and_keeps_changes_ineligible():
    conditions = {"epp": "declared", "require_epp_match": False}
    start = state()
    end = deepcopy(start)
    end["epp"] = "changed"
    end["cpu_policies"]["policies"][0]["energy_performance_preference"] = "changed"
    result = assess_environment(
        start, end, [{"monotonic_ns": 1, "snapshot": start}], conditions, []
    )
    assert "frozen_environment_mismatch:epp" not in result["reasons"]
    assert "environment_changed:epp" in result["reasons"]
    assert "cpu_policy_changed" in result["reasons"]
    assert not result["comparison_eligible"]
    assert (
        "frozen_environment_mismatch:epp"
        in assess_environment(
            start, end, [{"monotonic_ns": 1, "snapshot": start}], {"epp": "declared"}, []
        )["reasons"]
    )


def test_cpu_policy_opt_out_retains_unknown_and_other_mismatches():
    data = inventory()
    result = assess([data], {"epp": "declared", "require_epp_match": False, "governor": "other"})
    assert "cpu_policy_frozen_mismatch:epp" not in result["reasons"]
    assert "cpu_policy_frozen_mismatch:governor" in result["reasons"]
    data["policies"][0]["energy_performance_preference"] = None
    result = assess([data], {"epp": "declared", "require_epp_match": False})
    assert "cpu_policy_unknown:energy_performance_preference" in result["reasons"]
    assert not result["complete_and_stable"]


@pytest.mark.parametrize("invalid", ["false", 0, None])
def test_epp_switch_must_be_boolean(config_path, invalid):
    config = load_config(config_path).config.to_dict()
    config["conditions"]["require_epp_match"] = invalid
    with pytest.raises(ContractError, match="require_epp_match"):
        validate_runtime_config(config)


@pytest.mark.parametrize(
    "diagnostic,admission_policy",
    [
        (False, None),
        (True, None),
        (False, {"definition": "environment-admission.v2", "required_fields": []}),
        (True, {"definition": "environment-admission.v2", "required_fields": []}),
    ],
)
@pytest.mark.parametrize("periodic", [False, True])
def test_diagnostic_preflight_does_not_override_explicit_runtime_safety(
    diagnostic, admission_policy, periodic
):
    observed = dict(
        ac_online=False, profile="performance", governor="performance", epp="performance"
    )
    policy = dict(
        check_environment=True,
        max_temperature_celsius=90,
        require_temperature=False,
        max_external_cpu_percent=None,
    )
    config = {
        "conditions": dict(
            ac_online=True, profile="performance", governor="performance", epp="performance"
        )
    }
    guard = SafetyGuard(policy, config, lambda: observed, sensors=Sensors(99))
    # Former wiring must never influence explicit safety, even if reintroduced.
    guard.environment_diagnostic = diagnostic
    guard.environment_policy = admission_policy
    with pytest.raises(identity.PreflightError, match="temperature_safety_threshold_reached"):
        guard.check(periodic=periodic)
    guard.sensors = Sensors(40)
    with pytest.raises(identity.PreflightError, match="environment_safety_condition_changed"):
        guard.check(periodic=periodic)
    assert guard.last["environment"]["ac_online"] is False
    assert config["conditions"]["ac_online"] is True
    guard.policy["check_environment"] = False
    guard.check(periodic=periodic)
