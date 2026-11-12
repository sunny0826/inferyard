"""Frozen environment requirements; opt-outs do not suppress observations."""


def run_preflight(preflight, config, *, diagnostic=False, policy=None):
    from inferyard.platforms.identity import static_preflight

    if preflight is static_preflight:
        return preflight(config, diagnostic=diagnostic, environment_policy=policy)
    return preflight(config)
