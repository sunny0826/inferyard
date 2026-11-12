"""Frozen context-speed profile constants and pure experiment rules."""

from inferyard.errors import ContractError

PROFILE = "context-speed.v1"
BUNDLE_ID = "zh-context-speed"
BUNDLE_VERSION = "2026.10.1"
BUNDLE_CONTENT_SHA256 = "a92e8b9f9e108071d979efc598c3ed55a047220a893b9a8c85525357e6d31407"
CASES = {65536: "context-speed-64k", 131072: "context-speed-128k", 262144: "context-speed-256k"}
GENERATION = {
    "temperature": 0,
    "top_p": 1,
    "top_k": 1,
    "min_p": 0,
    "presence_penalty": 0,
    "repeat_penalty": 1,
    "seed": 0,
    "seed_support": "supported",
    "reasoning_mode": "off",
    "max_tokens": 256,
    "stop": [],
}


def require_profile(condition, reason):
    if not condition:
        raise ContractError("context_speed.profile", reason)


def validate_experiment_profile(experiment):
    profile = experiment.get("profile")
    if profile is None:
        return
    require_profile(profile == PROFILE, "unknown_profile")
    require_profile(len(experiment["workloads"]) == 1, "single_workload_required")
    execution = experiment["execution"]
    require_profile(
        execution["order"] == "fixed" and execution["seed"] is None, "fixed_order_required"
    )
    require_profile(
        execution["concurrency"] == 1 and execution["automatic_retries"] == 0,
        "serial_without_retry_required",
    )
    workload_profile(experiment["workloads"][0])


def workload_profile(workload):
    require_profile(
        workload["purpose"] == "performance" and workload["repeats"] == 5,
        "five_performance_repeats_required",
    )
    protocol = workload["protocol"]
    require_profile(
        protocol["kind"] == "fixed"
        and len(protocol["case_ids"]) == 1
        and protocol["case_ids"][0] in CASES.values(),
        "fixed_context_case_required",
    )
    require_profile(
        workload["input_target_tokens"] is None and workload.get("input_tolerance_tokens", 0) == 0,
        "actual_input_required",
    )
    require_profile(
        workload.get("output_mode") == "strict_fixed_length"
        and workload["output_budget_tokens"] == 256,
        "fixed_output_required",
    )
    require_profile(
        "cache_protocol" not in workload and "repeat_case_orders" not in workload,
        "profile_protocol_conflict",
    )
