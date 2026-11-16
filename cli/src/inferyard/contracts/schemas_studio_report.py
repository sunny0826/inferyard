"""Studio report contract: the single JSON projection consumed by the desktop panel."""

from inferyard.contracts.schemas_common import (
    BOOL,
    EXECUTION,
    IDENTIFIER,
    LABEL,
    NAT,
    array,
    enum,
    nullable,
    obj,
)
from inferyard.errors import ContractError

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]

KIND = "studio-report.v1"
CATEGORY = enum("instruction", "extraction", "qa", "math", "classification", "structured")
REQUEST_QUALITY = enum("pass", "fail", "unscorable", "not_applicable", "not_scored")
BACKEND = enum("cpu", "cuda", "metal")
NONNEGATIVE = {"type": "number", "minimum": 0}

SOURCE = obj({"run_id": IDENTIFIER, "path": LABEL})
FACETS = obj({"bundle_label": LABEL})
ENGINE = obj({"binary_path": nullable(LABEL), "adapter": LABEL, "backend": BACKEND})
MACHINE = obj(
    {
        "cpu": nullable(LABEL),
        "logical_cpus": nullable(NAT),
        "memory_total_bytes": nullable(NAT),
    }
)
PROFILE = obj({"title": nullable(LABEL), "engine": ENGINE, "machine": MACHINE})
COMPLETION = obj({"numerator": NAT, "denominator": NAT, "excluded": NAT})
SUMMARY = obj({"completion_rate": COMPLETION, "stop_reason": LABEL})
CATEGORY_RATE = obj({"id": CATEGORY, "label": LABEL, "rate": COMPLETION})
QUALITY = obj(
    {
        "numerator": NAT,
        "denominator": NAT,
        "excluded": NAT,
        "complete": BOOL,
        "reason": nullable(LABEL),
    }
)
LATENCY = obj(
    {
        "sample_count": NAT,
        "excluded": NAT,
        **{key: nullable(NONNEGATIVE) for key in ("min", "p50", "p95", "max")},
    }
)
DASHBOARD = obj(
    {
        "quality": QUALITY,
        "categories": {**array(CATEGORY_RATE), "maxItems": 6},
        "latency": LATENCY,
        "sampled_peak_rss_bytes": nullable(NONNEGATIVE),
        "sampled_peak_rss_reason": nullable(LABEL),
        "sampled_minimum_available_bytes": nullable(NONNEGATIVE),
        "minimum_available_reason": nullable(LABEL),
    }
)
THROUGHPUT = obj(
    {
        "requests_per_second": nullable(NONNEGATIVE),
        "requests_per_second_reason": nullable(LABEL),
        "output_tokens_per_second": nullable(NONNEGATIVE),
        "output_tokens_per_second_reason": nullable(LABEL),
        "goodput_per_second": nullable(NONNEGATIVE),
        "goodput_per_second_reason": nullable(LABEL),
    }
)
OUTPUT_BLOCK_INTERVALS = obj(
    {
        "interval_count": NAT,
        "request_count": NAT,
        "min": nullable(NONNEGATIVE),
        "p50": nullable(NONNEGATIVE),
        "p95": nullable(NONNEGATIVE),
        "p95_reason": nullable(LABEL),
        "max": nullable(NONNEGATIVE),
        "reason": nullable(LABEL),
    }
)
# Optional sections keep old reports readable; when present their fields are complete.
DASHBOARD["properties"]["throughput"] = THROUGHPUT
DASHBOARD["properties"]["output_block_intervals"] = OUTPUT_BLOCK_INTERVALS
REQUEST = obj(
    {
        "case_id": IDENTIFIER,
        "category_label": LABEL,
        "state": EXECUTION,
        "quality": nullable(REQUEST_QUALITY),
        "latency_ms": nullable(NONNEGATIVE),
    }
)
RUN = obj(
    {
        "source": SOURCE,
        "model": LABEL,
        "threads": nullable(NAT),
        "diagnostic": BOOL,
        "facets": FACETS,
        "profile": PROFILE,
        "summary": SUMMARY,
        "dashboard": DASHBOARD,
        "requests": array(REQUEST),
    }
)
STUDIO_REPORT = obj(
    {
        "kind": {"type": "string", "const": KIND},
        "runs": {**array(RUN, 1), "maxItems": 100},
    }
)


def _validate_categories(run: JsonObject, dashboard: JsonObject, path: str) -> None:
    categories = dashboard["categories"]
    ids = [category["id"] for category in categories]
    if len(ids) != len(set(ids)):
        raise ContractError(path + ".categories", "duplicate category")
    quality = dashboard["quality"]
    for key in ("numerator", "denominator", "excluded"):
        if quality[key] != sum(category["rate"][key] for category in categories):
            raise ContractError(f"{path}.quality.{key}", "must equal the sum of category counts")
    if quality["numerator"] > quality["denominator"]:
        raise ContractError(path + ".quality.numerator", "exceeds denominator")
    if (quality["reason"] is None) != quality["complete"]:
        raise ContractError(path + ".quality.reason", "required exactly when incomplete")
    completion = run["summary"]["completion_rate"]
    if completion["numerator"] > completion["denominator"]:
        raise ContractError(path + ".summary.completion_rate.numerator", "exceeds denominator")


def _validate_latency_counts(run: JsonObject, latency: JsonObject, path: str) -> None:
    if latency["sample_count"] + latency["excluded"] != len(run["requests"]):
        raise ContractError(path + ".latency", "counts must cover every request")


def _validate_empty_latency(latency: JsonObject, path: str) -> None:
    quantiles = [latency[key] for key in ("min", "p50", "p95", "max")]
    if any(value is not None for value in quantiles):
        raise ContractError(path + ".latency", "empty samples require null quantiles")


def _validate_latency_quantiles(latency: JsonObject, path: str) -> None:
    if latency["sample_count"] == 0:
        _validate_empty_latency(latency, path)
        return
    if any(latency[key] is None for key in ("min", "p50", "max")):
        raise ContractError(path + ".latency", "samples require min, p50 and max")
    if (latency["p95"] is None) != (latency["sample_count"] < 20):
        raise ContractError(path + ".latency.p95", "required from twenty samples")
    if not latency["min"] <= latency["p50"] <= latency["max"]:
        raise ContractError(path + ".latency", "quantiles are not ordered")
    if latency["p95"] is not None and not latency["p50"] <= latency["p95"] <= latency["max"]:
        raise ContractError(path + ".latency.p95", "quantiles are not ordered")


def _validate_latency(run: JsonObject, latency: JsonObject, path: str) -> None:
    _validate_latency_counts(run, latency, path)
    _validate_latency_quantiles(latency, path)


def _validate_memory(dashboard: JsonObject, path: str) -> None:
    for value_key, reason_key in (
        ("sampled_peak_rss_bytes", "sampled_peak_rss_reason"),
        ("sampled_minimum_available_bytes", "minimum_available_reason"),
    ):
        if (dashboard[value_key] is None) != (dashboard[reason_key] is not None):
            raise ContractError(f"{path}.{reason_key}", "required exactly when the value is missing")


def _validate_throughput(dashboard: JsonObject, path: str) -> None:
    throughput = dashboard.get("throughput")
    if throughput is None:
        return
    for value_key, reason_key in (
        ("requests_per_second", "requests_per_second_reason"),
        ("output_tokens_per_second", "output_tokens_per_second_reason"),
        ("goodput_per_second", "goodput_per_second_reason"),
    ):
        if (throughput[value_key] is None) != (throughput[reason_key] is not None):
            raise ContractError(
                f"{path}.throughput.{reason_key}",
                "required exactly when the value is missing",
            )


def _validate_interval_p95(intervals: JsonObject, count: int, path: str) -> None:
    name = path + ".output_block_intervals.p95"
    if count < 20:
        if intervals["p95"] is not None or intervals["p95_reason"] != "insufficient_samples":
            raise ContractError(name, "requires null with a sample reason below twenty")
    elif (
        intervals["p95"] is None
        or intervals["p95_reason"] is not None
        or not intervals["p50"] <= intervals["p95"] <= intervals["max"]
    ):
        raise ContractError(name, "required and ordered from twenty samples")


def _validate_empty_intervals(intervals: JsonObject, path: str) -> None:
    quantiles = [intervals[key] for key in ("min", "p50", "p95", "max")]
    if any(value is not None for value in quantiles) or intervals["reason"] is None:
        raise ContractError(
            path + ".output_block_intervals",
            "empty intervals require null quantiles and a reason",
        )
    _validate_interval_p95(intervals, 0, path)


def _validate_measured_intervals(intervals: JsonObject, count: int, path: str) -> None:
    if any(intervals[key] is None for key in ("min", "p50", "max")):
        raise ContractError(path + ".output_block_intervals", "samples require min, p50 and max")
    if intervals["reason"] is not None:
        raise ContractError(
            path + ".output_block_intervals.reason", "measured intervals have no reason"
        )
    if not intervals["min"] <= intervals["p50"] <= intervals["max"]:
        raise ContractError(path + ".output_block_intervals", "quantiles are not ordered")
    _validate_interval_p95(intervals, count, path)


def _validate_intervals(dashboard: JsonObject, path: str) -> None:
    intervals = dashboard.get("output_block_intervals")
    if intervals is None:
        return
    count = intervals["interval_count"]
    if count == 0:
        _validate_empty_intervals(intervals, path)
    else:
        _validate_measured_intervals(intervals, count, path)


def _validate_run(run: JsonObject, path: str) -> None:
    dashboard = run["dashboard"]
    _validate_categories(run, dashboard, path)

    latency = dashboard["latency"]
    _validate_latency(run, latency, path)

    _validate_memory(dashboard, path)

    _validate_throughput(dashboard, path)

    _validate_intervals(dashboard, path)


def validate_studio_report(data: JsonObject) -> None:
    """Enforce cross-field rules that JSON Schema cannot express."""
    run_ids = [run["source"]["run_id"] for run in data["runs"]]
    if len(run_ids) != len(set(run_ids)):
        raise ContractError("studio_report.runs", "duplicate source run")
    for index, run in enumerate(data["runs"]):
        _validate_run(run, f"studio_report.runs[{index}]")
