"""Frozen context-speed profile rules bound to bundle review and local cache policy."""

import re

from inferyard.config.cache_policy import disabled_startup
from inferyard.config.fixed_output import validate_inputs as validate_fixed_output
from inferyard.config.startup_arguments import argument_values, model_argument
from inferyard.contracts.context_speed_profile import (
    BUNDLE_CONTENT_SHA256,
    BUNDLE_ID,
    BUNDLE_VERSION,
    CASES,
    GENERATION,
    PROFILE,
    require_profile,
    validate_experiment_profile,
    workload_profile,
)


def classify_workload_inputs(workload, config, bundle, *, experiment_profile=None):
    """Return None or PROFILE; contradictory special sources always raise.

    Inputs must first pass their ordinary structural/semantic validation.
    Content checking is independent of (and does not grant) human review approval.
    """
    from inferyard.contracts.bundle import content_hash

    special = (
        bundle["bundle_id"] == BUNDLE_ID
        or any(c["case_id"] in CASES.values() for c in bundle["cases"])
        or any(c in CASES.values() for c in workload["protocol"]["case_ids"])
    )
    require_profile(experiment_profile in (None, PROFILE), "unknown_profile")
    if experiment_profile is None:
        require_profile(not special, "profile_missing")
        return None
    workload_profile(workload)
    require_profile(
        bundle["bundle_id"] == BUNDLE_ID
        and bundle["version"] == BUNDLE_VERSION
        and config["bundle"]["version"] == BUNDLE_VERSION
        and bundle.get("task_protocol") == "performance",
        "bundle_identity_mismatch",
    )
    require_profile(content_hash(bundle) == BUNDLE_CONTENT_SHA256, "bundle_content_mismatch")
    context = config["conditions"]["context_size"]
    require_profile(type(context) is int and context in CASES, "context_unsupported")
    require_profile(workload["protocol"]["case_ids"] == [CASES[context]], "case_context_mismatch")
    require_profile(config["engine"]["adapter"] == "prism_llama_server_v1", "engine_unsupported")
    require_profile(
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
    require_profile(
        execution["repeats"] == execution["concurrency"] == execution["warmup_count"] == 1
        and execution["order"] == "fixed"
        and not execution.get("require_fresh_process", False),
        "execution_recipe_mismatch",
    )
    require_profile(
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
    require_profile(len(results) == 1, "mixed_profile_sources")
    return results.pop()


def _startup(config):
    args = config["engine"]["startup_args"]
    require_profile(disabled_startup(args), "cold_cache_startup_required")
    require_profile(model_argument(args) is not None, "startup_model_ambiguous")
    for names, expected in (
        (("-c", "--ctx-size"), str(config["conditions"]["context_size"])),
        (("-np", "--parallel"), "1"),
        (("--reasoning",), "off"),
    ):
        require_profile(
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
        "--device",
        "--log-verbosity",
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
        require_profile(value is not None and not value.startswith("--"), "startup_value_missing")
        require_profile(index not in occupied and option not in seen, "startup_option_ambiguous")
        if option in {"--device", "--log-verbosity"}:
            require_profile(value != "", "startup_value_missing")
        seen.add(option)
        occupied.add(index)
        if "=" not in args[index]:
            occupied.add(index + 1)
    for index, arg in enumerate(args):
        if index in occupied:
            continue
        require_profile(arg in flags, "startup_option_unsupported")
        require_profile(arg not in seen, "startup_option_ambiguous")
        seen.add(arg)
    for _, option, value in argument_values(args, {"--device", "--log-verbosity"}):
        if option == "--device":
            require_profile(
                config["engine"]["backend"] == "cuda"
                and re.fullmatch(r"CUDA(0|[1-9][0-9]*)", value) is not None,
                "startup_recipe_mismatch",
            )
        else:
            require_profile(value == "4", "startup_recipe_mismatch")
