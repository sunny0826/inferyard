"""Frozen prefix-reuse inputs and explicit request policy."""

from inferyard.config.cache_policy import disabled_startup
from inferyard.contracts.validation import ContractError

PROTOCOL = "prism_prefix_reuse.v1"


def validate_inputs(workload, config):
    if "cache_protocol" not in workload:
        return
    if config["engine"]["adapter"] in ("kvmem", "ninfer"):
        raise ContractError("cache_protocol", "Prism prefix reuse requires a Prism adapter")
    args = config["engine"]["startup_args"]
    if (
        workload["protocol"]["kind"] != "fixed"
        or config["conditions"]["cache_policy"] not in ("disabled", "enabled")
        or not disabled_startup(args)
        or any(a.split("=", 1)[0] in ("--cache-reuse", "-cr") for a in args)
    ):
        raise ContractError(
            "cache_protocol", "requires fixed known policy and isolated prefix reuse"
        )


def request_value(config, phase, ordinal):
    return config["conditions"]["cache_policy"] == "enabled" and not (
        phase == "probe" and ordinal == 1
    )
