"""Pure frozen rules for the opt-in context speed experiment."""

from inferyard.config.cache_policy import disabled_startup
from inferyard.config.fixed_output import validate_inputs as validate_fixed_output
from inferyard.config.startup_arguments import argument_values, model_argument
from inferyard.contracts.validation import ContractError

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


def _require(condition, reason):
    if not condition:
        raise ContractError("context_speed.profile", reason)


def validate_experiment_profile(experiment):
    profile = experiment.get("profile")
    if profile is None:
        return
    _require(profile == PROFILE, "unknown_profile")
    _require(len(experiment["workloads"]) == 1, "single_workload_required")
    execution = experiment["execution"]
    _require(execution["order"] == "fixed" and execution["seed"] is None, "fixed_order_required")
    _require(
        execution["concurrency"] == 1 and execution["automatic_retries"] == 0,
        "serial_without_retry_required",
    )
    _workload(experiment["workloads"][0])


def _workload(workload):
    _require(
        workload["purpose"] == "performance" and workload["repeats"] == 5,
        "five_performance_repeats_required",
    )
    protocol = workload["protocol"]
    _require(
        protocol["kind"] == "fixed"
        and len(protocol["case_ids"]) == 1
        and protocol["case_ids"][0] in CASES.values(),
        "fixed_context_case_required",
    )
    _require(
        workload["input_target_tokens"] is None and workload.get("input_tolerance_tokens", 0) == 0,
        "actual_input_required",
    )
    _require(
        workload.get("output_mode") == "strict_fixed_length"
        and workload["output_budget_tokens"] == 256,
        "fixed_output_required",
    )
    _require(
        "cache_protocol" not in workload and "repeat_case_orders" not in workload,
        "profile_protocol_conflict",
    )


def classify_workload_inputs(workload, config, bundle, *, experiment_profile=None):
    """Return None or PROFILE; contradictory special sources always raise.

    Inputs must first pass their ordinary structural/semantic validation.
    Content checking is independent of (and does not grant) human review approval.
    """
    from inferyard.config.bundle import content_hash

    special = (
        bundle["bundle_id"] == BUNDLE_ID
        or any(c["case_id"] in CASES.values() for c in bundle["cases"])
        or any(c in CASES.values() for c in workload["protocol"]["case_ids"])
    )
    _require(experiment_profile in (None, PROFILE), "unknown_profile")
    if experiment_profile is None:
        _require(not special, "profile_missing")
        return None
    _workload(workload)
    _require(
        bundle["bundle_id"] == BUNDLE_ID
        and bundle["version"] == BUNDLE_VERSION
        and config["bundle"]["version"] == BUNDLE_VERSION
        and bundle.get("task_protocol") == "performance",
        "bundle_identity_mismatch",
    )
    _require(content_hash(bundle) == BUNDLE_CONTENT_SHA256, "bundle_content_mismatch")
    context = config["conditions"]["context_size"]
    _require(type(context) is int and context in CASES, "context_unsupported")
    _require(workload["protocol"]["case_ids"] == [CASES[context]], "case_context_mismatch")
    _require(config["engine"]["adapter"] == "prism_llama_server_v1", "engine_unsupported")
    _require(
        all(
            type(config["generation"].get(k)) is type(v)
            or type(v) in (int, float)
            and type(config["generation"].get(k)) in (int, float)
            for k, v in GENERATION.items()
        )
        and all(config["generation"].get(k) == v for k, v in GENERATION.items()),
        "generation_recipe_mismatch",
    )
    execution = config["execution"]
    _require(
        execution["repeats"] == execution["concurrency"] == execution["warmup_count"] == 1
        and execution["order"] == "fixed"
        and not execution.get("require_fresh_process", False),
        "execution_recipe_mismatch",
    )
    _require(
        config["conditions"]["slots"] == 1 and config["conditions"]["cache_policy"] == "disabled",
        "cold_serial_required",
    )
    _startup(config)
    validate_fixed_output(workload, config, bundle)
    return PROFILE


def classify_verified_inputs(plan, config, bundle):
    """Classify already verified inputs for downstream output admission."""
    validate_experiment_profile(plan["experiment"])
    results = {
        classify_workload_inputs(
            w, config, bundle, experiment_profile=plan["experiment"].get("profile")
        )
        for w in plan["experiment"]["workloads"]
    }
    _require(len(results) == 1, "mixed_profile_sources")
    return results.pop()


def _startup(config):
    args = config["engine"]["startup_args"]
    _require(disabled_startup(args), "cold_cache_startup_required")
    _require(model_argument(args) is not None, "startup_model_ambiguous")
    for names, expected in (
        (("-c", "--ctx-size"), str(config["conditions"]["context_size"])),
        (("-np", "--parallel"), "1"),
        (("--reasoning",), "off"),
    ):
        _require(
            [v for _, _, v in argument_values(args, names)] == [expected], "startup_recipe_mismatch"
        )
    # Closed supported startup vocabulary excludes draft/lookup/ngram routes.
    valued = {
        "-m",
        "--model",
        "-ngl",
        "--gpu-layers",
        "--n-gpu-layers",
        "-t",
        "--threads",
        "-tb",
        "--threads-batch",
        "-c",
        "--ctx-size",
        "-np",
        "--parallel",
        "--host",
        "--port",
        "--reasoning",
        "--chat-template-kwargs",
        "--cache-ram",
        "-cram",
        "-b",
        "--batch-size",
        "-ub",
        "--ubatch-size",
        "-fa",
        "--flash-attn",
        "-ctk",
        "--cache-type-k",
        "-ctv",
        "--cache-type-v",
    }
    flags = {
        "--no-webui",
        "--jinja",
        "--no-cache-prompt",
        "--no-cache-idle-slots",
        "--slots",
        "--no-mmap",
        "--mlock",
        "--no-warmup",
        "--no-context-shift",
        "--metrics",
    }
    occupied, seen = set(), set()
    for index, option, value in argument_values(args, valued):
        _require(value is not None and not value.startswith("--"), "startup_value_missing")
        _require(index not in occupied and option not in seen, "startup_option_ambiguous")
        seen.add(option)
        occupied.add(index)
        if "=" not in args[index]:
            occupied.add(index + 1)
    for index, arg in enumerate(args):
        if index in occupied:
            continue
        _require(arg in flags, "startup_option_unsupported")
        _require(arg not in seen, "startup_option_ambiguous")
        seen.add(arg)
