"""Frozen environment requirements; opt-outs do not suppress observations."""


def requires_match(conditions, field):
    return field != "epp" or conditions.get("require_epp_match", True)


def mismatches(conditions, environment):
    allowed_unknown = set()
    if conditions.get("allow_unknown_environment", False):
        if environment.get("platform") == "Windows":
            allowed_unknown = {"ac_online", "profile", "governor", "epp"}
        elif environment.get("platform") == "Darwin":
            allowed_unknown = {"profile", "governor", "epp"}
    result = [
        field
        for field in ("ac_online", "profile", "governor", "epp")
        if requires_match(conditions, field)
        and not (field in allowed_unknown and environment.get(field) is None)
        and (environment.get(field) is None or environment[field] != conditions[field])
    ]
    if "macos_power_policy" in conditions:
        from inferyard.platforms.power_macos import valid

        observed = environment.get("macos_power_policy")
        if (
            environment.get("platform") != "Darwin"
            or not valid(observed)
            or observed != conditions["macos_power_policy"]
        ):
            result.append("macos_power_policy")
    return result


def admission(conditions, environment, *, diagnostic=False, policy=None):
    """New explicit policy affects admission only; observations remain unfiltered."""
    legacy_blockers = mismatches(conditions, environment)
    source = "plan" if policy is not None else "config"
    policy = policy if policy is not None else conditions.get("environment_admission")
    fields = ("ac_online", "profile", "governor", "epp", "macos_power_policy")
    differences = [
        field
        for field in fields
        if field in conditions
        and (environment.get(field) is None or environment.get(field) != conditions[field])
    ]
    required = policy["required_fields"] if policy is not None else legacy_blockers
    blockers = (
        legacy_blockers
        if policy is None
        else [
            field
            for field in required
            if environment.get(field) is None or environment.get(field) != conditions.get(field)
        ]
    )
    return {
        "definition": "environment-admission.v2",
        "diagnostic": diagnostic,
        "policy_source": source if policy is not None else "legacy",
        "policy": policy,
        "differences": differences,
        "required_fields": list(required),
        "blockers": [] if diagnostic else blockers,
    }


def run_preflight(preflight, config, *, diagnostic=False, policy=None):
    from inferyard.platforms.identity import static_preflight

    if preflight is static_preflight:
        return preflight(config, diagnostic=diagnostic, environment_policy=policy)
    return preflight(config)
