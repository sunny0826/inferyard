from inferyard.errors import PreflightError
"""Admission for the initial serial local-engine profile."""


def safety_policy(plan, config):
    policy = plan["experiment"].get("safety")
    required = config["safety"]
    if (
        policy is None
        or policy["interval_seconds"] > required["interval_seconds"]
        or (required["check_environment"] and policy["check_environment"] is not True)
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
