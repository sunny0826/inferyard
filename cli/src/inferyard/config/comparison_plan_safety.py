"""Declared comparison overhead, safety policy and bundle content checks."""

from __future__ import annotations

import hashlib

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.config.loader import LoadedConfig
from inferyard.contracts.validation import ContractError


def comparison_overhead(config: JSONRecord, count: int) -> float:
    execution, telemetry = config["execution"], config["telemetry"]
    auxiliary = 2 + execution["warmup_count"]
    return (
        auxiliary * execution["timeout_seconds"]
        + (count + auxiliary + 1) * execution["idle_wait_seconds"]
        + telemetry["baseline_seconds"]
    )


def comparison_safety_overrides(policies: list[JSONRecord]) -> list[bool]:
    overrides = ["temperature_stop_override_reason" in policy for policy in policies]
    if any(overrides) and not all(overrides):
        raise ContractError(
            "compare_plan.safety",
            "temperature stop override must cover every comparison config",
        )
    return overrides


def _max_temperature(policies: list[JSONRecord], overrides: list[bool]) -> float | None:
    if all(overrides):
        return None
    return min(policy["max_temperature_celsius"] for policy in policies)


def comparison_safety(configs: list[JSONRecord]) -> JSONRecord | None:
    if not all("safety" in config for config in configs):
        if any("safety" in config for config in configs):
            raise ContractError(
                "compare_plan.profile",
                "mixed local and external engine profiles are not comparable",
            )
        return None
    policies = [config["safety"] for config in configs]
    overrides = comparison_safety_overrides(policies)
    return {
        "interval_seconds": min(policy["interval_seconds"] for policy in policies),
        "max_temperature_celsius": _max_temperature(policies, overrides),
        "require_temperature": all(policy["require_temperature"] for policy in policies),
        "check_environment": all(policy["check_environment"] for policy in policies),
        "max_external_cpu_percent": None,
    }


def comparison_budget(configs: list[JSONRecord], limits: list[tuple[int, float]]) -> JSONRecord:
    return {
        "max_requests": sum(limit for limit, _ in limits),
        "max_wall_seconds": sum(total for _, total in limits),
        "min_disk_bytes": max(config["output"]["min_disk_bytes"] for config in configs),
        "min_available_memory_bytes": max(
            config["output"]["min_available_memory_bytes"] for config in configs
        ),
    }


def require_bundle_content(
    left: dict[str, tuple[LoadedConfig, bytes, JSONRecord]],
    right: dict[str, tuple[LoadedConfig, bytes, JSONRecord]],
    expected: dict[str, tuple[bytes, JSONRecord]],
) -> None:
    for key, (raw, _) in expected.items():
        for label, side in (("left", left), ("right", right)):
            if hashlib.sha256(side[key][1]).hexdigest() != hashlib.sha256(raw).hexdigest():
                raise ContractError(
                    "compare_plan.bundle",
                    f"{label} config bundle content differs for {key}",
                )


