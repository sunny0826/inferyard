"""Admission for the initial serial local-engine profile."""

from inferyard.platforms.identity import PreflightError


def _temperature_admitted(policy, required):
    # Only a frozen operator override reason permits a null stop threshold;
    # the temperature source stays mandatory either way.
    if "temperature_stop_override_reason" in required:
        return policy["max_temperature_celsius"] is None
    return (
        policy["max_temperature_celsius"] is not None
        and policy["max_temperature_celsius"] <= required["max_temperature_celsius"]
    )


def safety_policy(plan, config):
    policy = plan["experiment"].get("safety")
    required = config["safety"]
    if (
        policy is None
        or not _temperature_admitted(policy, required)
        or policy["interval_seconds"] > required["interval_seconds"]
        or policy["require_temperature"] is not True
        or policy["check_environment"] is not True
    ):
        raise PreflightError("local_engine_safety_policy_required")
    for workload in plan["experiment"]["workloads"]:
        if (
            workload["protocol"]["kind"] != "fixed"
            or workload.get("input_target_tokens") is not None
            or workload.get("cache_protocol") is not None
            or workload.get("output_mode") == "strict_fixed_length"
        ):
            raise PreflightError("local_engine_protocol_not_supported")
    return policy
