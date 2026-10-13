"""Semantic checks for independent diagnostic engine-fit evidence."""

from collections import Counter

from inferyard.reporting.engine_fit_binding_validation import validate_binding as _binding
from inferyard.reporting.engine_fit_validation_primitives import (
    digest as _digest,
)
from inferyard.reporting.engine_fit_validation_primitives import (
    fields,
    number,
    require,
    text,
)

STATUSES = ("completed", "failed", "cancelled", "invalid", "not_executed")
RUN_FIELDS = {
    "schema_version",
    "definition",
    "run_id",
    "plan_id",
    "engine",
    "diagnostic",
    "performance_comparison_qualified",
    "completeness",
    "stop_reason",
    "binding",
    "service",
    "resources",
    "counts",
    "limitations",
}
RESPONSE_FIELDS = {
    "text",
    "finish_reason",
    "elapsed_ms",
    "first_content_ms",
    "prompt_tokens",
    "completion_tokens",
    "usage_missing_reason",
}
RESOURCE_FIELDS = {
    "phase",
    "memory_available_bytes",
    "process_tree_rss_bytes",
    "process_tree_cpu_seconds",
    "process_count",
    "scope",
    "missing_reasons",
}
TEMPERATURE_FIELDS = {"temperature_samples", "temperature_missing_reason"}


def _response(value):
    fields(value, RESPONSE_FIELDS, "response_fields")
    require(type(value["text"]) is str, "response_text")
    require(value["finish_reason"] in ("stop", "length"), "response_finish")
    require(number(value["elapsed_ms"]), "response_elapsed")
    first = value["first_content_ms"]
    require(
        first is None or (number(first) and first <= value["elapsed_ms"]),
        "response_first_content",
    )
    require(bool(value["text"]) == (first is not None), "response_content_timing")
    tokens = [value["prompt_tokens"], value["completion_tokens"]]
    require(all(token is None or number(token, integer=True) for token in tokens), "response_usage")
    missing = any(token is None for token in tokens)
    require(
        text(value["usage_missing_reason"]) if missing else value["usage_missing_reason"] is None,
        "response_usage_reason",
    )


def _temperatures(value, *, macos, windows=False):
    if windows:
        from inferyard.reporting.engine_fit_windows_validation import temperatures

        temperatures(value)
        return
    samples = value["temperature_samples"]
    require(type(samples) is list, "temperature_samples")
    observed = False
    for sample in samples:
        fields(
            sample,
            {
                "collector",
                "server_pid",
                "process_start_ticks",
                "phase",
                "request_id",
                "metric_name",
                "source",
                "unit",
                "semantics",
                "raw_value",
                "value",
                "missing_reason",
                "read_started_ns",
                "read_finished_ns",
            },
            "temperature_sample",
        )
        require(
            sample.get("metric_name") == "temperature" and sample.get("unit") == "celsius",
            "temperature_unit",
        )
        require(
            sample["collector"] == ("macos-smc.v1" if macos else "linux-sensors.v2")
            and sample["semantics"] == ("smc_key_reported" if macos else "thermal_zone_reported")
            and text(sample["source"])
            and (not macos or sample["source"].startswith("AppleSMC:")),
            "temperature_source",
        )
        require(
            sample["server_pid"] is None
            and sample["process_start_ticks"] is None
            and sample["request_id"] is None
            and sample["phase"] == "engine_fit",
            "temperature_attribution",
        )
        require(
            number(sample["read_started_ns"], integer=True)
            and number(sample["read_finished_ns"], integer=True)
            and sample["read_finished_ns"] >= sample["read_started_ns"],
            "temperature_clock",
        )
        raw, measure = sample.get("raw_value"), sample.get("value")
        require("value" in sample and "raw_value" in sample, "temperature_fields")
        if measure is None:
            require(raw is None and text(sample.get("missing_reason")), "temperature_missing")
        else:
            require(type(measure) in (int, float) and number(abs(measure)), "temperature_value")
            if macos:
                require(
                    type(raw) in (int, float) and -273.15 <= raw <= 200 and measure == raw,
                    "temperature_conversion",
                )
            else:
                require(
                    type(raw) is int and -273150 <= raw <= 2**63 - 1 and measure == raw * 0.001,
                    "temperature_conversion",
                )
            require(sample.get("missing_reason") is None, "temperature_reason")
            observed = True
    reason = value["temperature_missing_reason"]
    require(reason is None if observed else text(reason), "temperature_missing_reason")


def _idle(value, engine, instance=None):
    fields(value, {"idle", "source", "values"}, "idle_fields")
    source = "lms:ps" if engine == "lmstudio" else "/metrics"
    require(type(value["idle"]) is bool and value["source"] == source, "idle_source")
    required = {
        "vllm": ("vllm:num_requests_running", "vllm:num_requests_waiting"),
        "sglang": ("sglang:num_running_reqs", "sglang:num_queue_reqs"),
        "llama-cpp": ("llamacpp:requests_processing", "llamacpp:requests_deferred"),
        "mlx-lm": (
            "engine_fit:num_requests_running",
            "engine_fit:num_requests_waiting",
            "engine_fit:poisoned",
        ),
        "lmstudio": ("lmstudio:queued", "lmstudio:active"),
    }[engine]
    optional = {"vllm": "vllm:num_requests_swapped", "sglang": "sglang:num_grammar_queue_reqs"}
    require(type(value["values"]) is list, "idle_values")
    identities, labels_by_metric, numbers = set(), {}, []
    for row in value["values"]:
        fields(row, {"metric", "labels", "value"}, "idle_metric_fields")
        metric = row["metric"]
        require(type(metric) is str and metric in (*required, optional.get(engine)), "idle_metric")
        require(type(row["labels"]) is dict, "idle_labels")
        require(all(text(k) and type(v) is str for k, v in row["labels"].items()), "idle_labels")
        if engine == "lmstudio":
            require(row["labels"] == {"instance": instance}, "idle_instance")
        labels = tuple(sorted(row["labels"].items()))
        identity = (metric, labels)
        require(identity not in identities, "idle_duplicate_series")
        identities.add(identity)
        labels_by_metric.setdefault(metric, set()).add(labels)
        observed = row["value"]
        require(number(observed) and observed == int(observed), "idle_metric_value")
        if metric == "lmstudio:active":
            require(observed in (0, 1), "idle_active")
        numbers.append(observed)
    require(all(labels_by_metric.get(metric) for metric in required), "idle_missing_metrics")
    require(
        all(labels_by_metric[required[0]] == labels_by_metric[key] for key in required[1:]),
        "idle_series_mismatch",
    )
    require(value["idle"] == all(observed == 0 for observed in numbers), "idle_value_mismatch")


def _resources(values, request_ids, engine, *, macos, windows=False, instance=None):
    require(type(values) is list, "resources_type")
    for value in values:
        require(
            type(value) is dict
            and RESOURCE_FIELDS <= value.keys()
            and value.keys() <= RESOURCE_FIELDS | TEMPERATURE_FIELDS | {"idle", "disk_free_bytes"},
            "resource_fields",
        )
        phases = {"before_run"} | {
            f"{p}:{r}" for p in ("before", "after", "stop") for r in request_ids
        }
        require(type(value["phase"]) is str and value["phase"] in phases, "resource_phase")
        scope = value["scope"]
        require(type(scope) is dict and bool(scope), "resource_scope")
        require(
            all(text(k) and (text(v) or number(v, integer=True)) for k, v in scope.items()),
            "resource_scope",
        )
        reasons = value["missing_reasons"]
        require(type(reasons) is dict, "resource_reasons")
        measures = RESOURCE_FIELDS - {"phase", "scope", "missing_reasons"}
        require(set(reasons) <= measures, "resource_reason_fields")
        for key in measures:
            observed = value[key]
            require(
                observed is None or number(observed, integer=key != "process_tree_cpu_seconds"),
                "resource_value",
            )
            if observed is None:
                require(text(reasons.get(key)), "resource_missing_reason")
            else:
                require(reasons.get(key) is None, "resource_unexpected_reason")
        if windows:
            from inferyard.reporting.engine_fit_windows_validation import resources

            resources(value)
        if TEMPERATURE_FIELDS & value.keys():
            require(TEMPERATURE_FIELDS <= value.keys(), "temperature_fields")
            _temperatures(value, macos=macos, windows=windows)
        if "idle" in value:
            _idle(value["idle"], engine, instance)
        if "disk_free_bytes" in value:
            require(number(value["disk_free_bytes"], integer=True), "disk_free_bytes")


def validate_run(plan, run, rows):
    """Reject plausible-looking summaries that disagree with their frozen rows."""
    from inferyard.config.engine_fit import request_rows

    require(type(run) is dict, "run_fields")
    modern = run.get("definition") in (
        "engine_fit_run.v3",
        "engine_fit_run.v4",
        "engine_fit_run.v5",
        "engine_fit_run.v6",
    )
    from inferyard.evidence.formats import require_core, require_version

    require_core(run, "engine-fit run")
    require_version(
        run, "definition", tuple(f"engine_fit_run.v{i}" for i in range(1, 7)), "engine-fit run"
    )
    fields(run, RUN_FIELDS | ({"platform"} if modern else set()), "run_fields")
    windows = run["definition"] == "engine_fit_run.v6"
    if windows:
        require(
            plan["definition"]
            in ("engine_fit_plan.v2", "engine_fit_plan.v3", "engine_fit_plan.v4"),
            "run_plan_definition",
        )
        require(
            run["platform"] == "Windows"
            and run["engine"] == "llama-cpp"
            and plan["model"].get("kind") == "gguf",
            "windows_engine_platform",
        )
    else:
        require(
            plan["definition"]
            == {
                "engine_fit_run.v1": "engine_fit_plan.v1",
                "engine_fit_run.v2": "engine_fit_plan.v1",
                "engine_fit_run.v3": "engine_fit_plan.v2",
                "engine_fit_run.v4": "engine_fit_plan.v3",
                "engine_fit_run.v5": "engine_fit_plan.v4",
            }[run["definition"]],
            "run_plan_definition",
        )
    if modern:
        require(
            run["platform"] in (("Windows",) if windows else ("Darwin", "Linux")), "run_platform"
        )
        require(run["platform"] == plan["host"].get("platform"), "run_platform")
    macos = run["definition"] == "engine_fit_run.v2" or (modern and run["platform"] == "Darwin")
    if macos:
        require(plan["host"].get("platform") == "Darwin", "run_platform")
    require(run["diagnostic"] is True, "diagnostic_required")
    require(run["performance_comparison_qualified"] is False, "performance_not_qualified")
    require(text(run["run_id"]), "run_id")
    require(run["plan_id"] == plan["plan_id"], "plan_mismatch")
    require(run["engine"] in plan["engines"], "engine_mismatch")
    require(run["engine"] != "ollama", "engine_execution_unavailable")
    require(run["engine"] not in ("mlx-lm", "lmstudio") or macos, "engine_platform")
    require(run["completeness"] in ("complete", "incomplete"), "completeness")
    require(run["stop_reason"] is None or text(run["stop_reason"]), "stop_reason")
    require(type(run["limitations"]) is list, "limitations")
    require(all(text(item) for item in run["limitations"]), "limitations")
    if windows:
        from inferyard.config.engine_fit_native_sources import WINDOWS_LIMITATIONS

        require(set(WINDOWS_LIMITATIONS) <= set(run["limitations"]), "windows_limitations")
        require(
            "model_binding_is_startup_directory_not_observed_gpu_residency"
            not in run["limitations"],
            "windows_limitations",
        )
    disabled_reasons = {}
    for measure, reasons in (
        ("temperature", ("temperature_safety_threshold_reached",)),
        ("memory", ("memory_safety_threshold_reached", "system_memory_unavailable")),
    ):
        disabled = plan["parameters"].get(measure + "_stop_override_reason") is not None
        require(
            (measure + "_stop_explicitly_disabled" in run["limitations"]) == disabled,
            measure + "_override",
        )
        if disabled:
            require(run["stop_reason"] not in reasons, measure + "_override")
            disabled_reasons.update(dict.fromkeys(reasons, measure))
    _binding(run["binding"], plan, run["engine"], macos=macos, windows=windows)
    fields(
        run["service"],
        {
            "engine",
            "version",
            "served_model",
            "version_source",
            "measurement_source_sha256",
            "idle_before",
        }
        | ({"version_missing_reason"} if modern else set()),
        "service",
    )
    require(run["service"]["engine"] == run["engine"], "service_engine")
    require(
        all(
            text(v)
            for k, v in run["service"].items()
            if k not in ("idle_before", "version", "version_missing_reason")
        ),
        "service_identity",
    )
    if run["engine"] == "lmstudio":
        require(
            run["service"]["version"] is None
            and run["service"]["version_missing_reason"] == "lmstudio_service_version_not_exposed",
            "service_version_missing",
        )
    else:
        require(text(run["service"]["version"]), "service_version")
        require(run["service"].get("version_missing_reason") is None, "service_version_reason")
    require(_digest(run["service"]["measurement_source_sha256"]), "measurement_source")
    require(
        run["service"]["version_source"]
        == {
            "vllm": "/version",
            "sglang": "/get_server_info",
            "llama-cpp": "/props",
            "mlx-lm": "/version",
            "lmstudio": "not_exposed",
        }[run["engine"]],
        "version_source",
    )
    instance = run["binding"].get("observer", {}).get("instance_id")
    if instance is not None:
        require(instance == run["service"]["served_model"], "service_instance")
    _idle(run["service"]["idle_before"], run["engine"], instance)
    require(run["service"]["idle_before"]["idle"] is True, "service_not_initially_idle")
    require(type(rows) is list and len(rows) == plan["request_count"], "request_count")
    expected = request_rows(plan)
    _resources(
        run["resources"],
        [row["request_id"] for row in expected],
        run["engine"],
        macos=macos,
        windows=windows,
        instance=instance,
    )
    for row, frozen in zip(rows, expected, strict=True):
        fields(row, {"request_id", "case_id", "status", "reason", "response"}, "row_fields")
        require(row["request_id"] == frozen["request_id"], "request_order")
        require(row["case_id"] == frozen["case_id"], "case_order")
        require(type(row["status"]) is str and row["status"] in STATUSES, "request_status")
        if row["status"] == "completed":
            require(row["reason"] is None, "completed_reason")
            require(run["binding"] is not None and run["service"] is not None, "completed_identity")
            _response(row["response"])
        else:
            require(text(row["reason"]), "request_missing_reason")
            require(row["response"] is None, "noncompleted_response")
        require(
            row["reason"] not in disabled_reasons,
            disabled_reasons.get(row["reason"], "") + "_override",
        )
    counts = run["counts"]
    fields(counts, {*STATUSES, "planned"}, "counts_fields")
    require(all(number(value, integer=True) for value in counts.values()), "counts_type")
    actual = Counter(row["status"] for row in rows)
    require(counts["planned"] == len(rows), "counts_planned")
    require(all(counts[status] == actual[status] for status in STATUSES), "counts_mismatch")
    if run["completeness"] == "complete":
        require(counts["completed"] == len(rows), "false_complete")
        require(run["stop_reason"] is None, "complete_stop_reason")
    else:
        require(text(run["stop_reason"]), "incomplete_stop_reason")
