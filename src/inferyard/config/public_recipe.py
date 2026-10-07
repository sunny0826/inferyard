"""Bounded public reproduction conditions; unknown text remains locally required."""

import hashlib

from inferyard.config.startup_portability import portable_startup
from inferyard.evidence.storage import EvidenceError, json_bytes


def fingerprint(value):
    return hashlib.sha256(json_bytes(value)).hexdigest()


def reproduction_recipe(data, numeric):
    config = data["config"]
    experiment = data["plan"]["experiment"]
    trial = next(t for t in data["plan"]["trials"] if t["trial_id"] == data["run"]["trial_id"])
    workload = next(w for w in experiment["workloads"] if w["workload_id"] == trial["workload_id"])
    required = {}

    def choice(section, key, choices):
        value = config[section][key]
        if value in choices:
            return value
        required[f"{section}.{key}"] = fingerprint(value)
        return None

    def boolean(value):
        if type(value) is not bool:
            raise EvidenceError("invalid_public_boolean_field")
        return value

    def numeric_fields(record, fields):
        return {k: numeric(record[k]) for k in fields if k in record}

    for section, key in (
        ("generation", "stop"),
        ("execution", "probe_prompt"),
        ("execution", "warmup_prompt"),
        ("conditions", "background_load"),
        ("engine", "startup_args"),
    ):
        required[f"{section}.{key}"] = fingerprint(config[section][key])
    cases = {c["case_id"]: (i, fingerprint(c)) for i, c in enumerate(data["bundle"]["cases"])}
    protocol = workload["protocol"]
    result = {
        "generation": {
            "reasoning_mode": choice("generation", "reasoning_mode", ("off", "on")),
            "seed_support": choice(
                "generation", "seed_support", ("supported", "unsupported", "unknown")
            ),
            "stop_sequences_empty": config["generation"]["stop"] == [],
        },
        "conditions": {
            "cache_policy": choice(
                "conditions", "cache_policy", ("disabled", "enabled", "unknown")
            ),
            "profile": choice("conditions", "profile", ("balanced", "performance", "power-saver")),
            "governor": choice(
                "conditions",
                "governor",
                ("powersave", "performance", "schedutil", "ondemand", "conservative", "userspace"),
            ),
            "epp": choice(
                "conditions",
                "epp",
                ("performance", "balance_performance", "balance_power", "power", "default"),
            ),
            "ac_online": boolean(config["conditions"]["ac_online"]),
            "model_loaded": boolean(config["conditions"]["model_loaded"]),
        },
        "execution": numeric_fields(
            config["execution"],
            ("concurrency", "repeats", "timeout_seconds", "warmup_count", "idle_wait_seconds"),
        ),
        "telemetry": numeric_fields(config["telemetry"], ("interval_ms", "baseline_seconds")),
        "plan_execution": {
            **numeric_fields(experiment["execution"], ("concurrency", "automatic_retries", "seed")),
            "order": experiment["execution"]["order"],
        },
        "budget": numeric_fields(
            experiment["budget"],
            ("max_requests", "max_wall_seconds", "min_disk_bytes", "min_available_memory_bytes"),
        ),
        "workload": {
            **numeric_fields(
                workload,
                (
                    "repeats",
                    "timeout_seconds",
                    "overhead_budget_seconds",
                    "input_target_tokens",
                    "input_tolerance_tokens",
                    "output_budget_tokens",
                ),
            ),
            "protocol_kind": protocol["kind"],
            "purpose": workload["purpose"],
            "repeat_index": numeric(trial["repeat_index"]),
            "case_order": [
                {"bundle_index": cases[c][0], "case_sha256": cases[c][1]}
                for c in trial["case_order"]
            ],
            **numeric_fields(
                protocol,
                (
                    "duration_seconds",
                    "max_requests",
                    "window_seconds",
                    "min_completed_per_case_per_window",
                    "drain_timeout_seconds",
                ),
            ),
        },
        "locally_required_sha256": required,
        "limitations": [
            "local_artifacts_required",
            "hashes_do_not_reveal_redacted_values",
            "device_and_environment_equivalence_not_proven",
        ],
    }
    if "performance_environment" in experiment:
        result["performance_environment"] = numeric_fields(
            experiment["performance_environment"],
            ("max_external_cpu_percent", "max_external_interval_seconds"),
        )
    if "capacity_stop" in experiment:
        result["capacity_stop"] = experiment["capacity_stop"]
    if "safety" in experiment:
        safety = experiment["safety"]
        result["safety"] = {
            **numeric_fields(
                safety, ("interval_seconds", "max_temperature_celsius", "max_external_cpu_percent")
            ),
            "require_temperature": boolean(safety["require_temperature"]),
            "check_environment": boolean(safety["check_environment"]),
        }
    else:
        result["safety"] = None
    required.pop("engine.startup_args")
    result["startup"] = portable_startup(config)
    result["workload"]["repeat_case_orders"] = [
        [{"bundle_index": cases[c][0], "case_sha256": cases[c][1]} for c in t["case_order"]]
        for t in data["plan"]["trials"]
        if t["workload_id"] == trial["workload_id"]
    ]
    result["definitions"] = {
        k: v
        for k, v in data["run"]["definition_versions"].items()
        if v in ("phase2.v1", "phase2.v2")
    }
    result["workload"]["position_cases"] = [
        {
            **{k: v for k, v in r.items() if k not in ("case_id", "family_id")},
            "bundle_index": cases[r["case_id"]][0],
            "family_id": "family-" + fingerprint(r["family_id"])[:32],
        }
        for r in workload.get("position_cases", [])
    ]
    return result
