"""Strict offline Windows observation units, sources and completeness."""

import re

from inferyard.config.engine_fit_native_sources import WINDOWS_SCOPE
from inferyard.reporting.engine_fit_validation_primitives import fields, number, require, text

SAMPLE_FIELDS = {
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
}


def temperatures(value):
    samples = value["temperature_samples"]
    require(type(samples) is list and bool(samples), "temperature_samples")
    observed, seen = False, set()
    for sample in samples:
        fields(sample, SAMPLE_FIELDS, "temperature_sample")
        source = sample["source"]
        require(
            sample["collector"] == "windows-nvidia-temperature.v1"
            and sample["semantics"] == "gpu_temperature_reported"
            and sample["metric_name"] == "temperature"
            and sample["unit"] == "celsius"
            and type(source) is str
            and (
                source == "nvidia-smi:unavailable"
                or re.fullmatch(r"nvidia-smi:gpu:(?:0|[1-9][0-9]{0,4}):temperature\.gpu", source)
                is not None
            ),
            "temperature_source",
        )
        require(source not in seen, "temperature_duplicate_source")
        seen.add(source)
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
        raw, measure = sample["raw_value"], sample["value"]
        if measure is None:
            require(raw is None and text(sample["missing_reason"]), "temperature_missing")
        else:
            require(
                source != "nvidia-smi:unavailable"
                and type(raw) in (int, float)
                and type(measure) in (int, float)
                and number(abs(measure))
                and -273.15 <= measure <= 200
                and raw == measure,
                "temperature_value",
            )
            require(sample["missing_reason"] is None, "temperature_reason")
            observed = True
    if "nvidia-smi:unavailable" in seen:
        require(len(samples) == 1, "temperature_source")
    reason = value["temperature_missing_reason"]
    require(reason is None if observed else text(reason), "temperature_missing_reason")


def resources(value):
    require(value["scope"] == WINDOWS_SCOPE, "windows_resource_scope")
    require(
        {"temperature_samples", "temperature_missing_reason"} <= value.keys(),
        "temperature_fields",
    )
    fields = ("process_tree_rss_bytes", "process_tree_cpu_seconds", "process_count")
    missing = [value[key] is None for key in fields]
    require(all(missing) or not any(missing), "windows_partial_process_tree")
    if not any(missing):
        require(value["process_count"] >= 1, "windows_process_count")
