"""Validate public observation structure without claiming source authenticity."""

import math
import re
from collections.abc import Set

from inferyard.analysis.public_metrics import NUMERIC, TEXT, VOCABULARY, fingerprint, is_reviewed
from inferyard.errors import EvidenceError
from inferyard.registry import catalogue

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]


def _require(condition: bool) -> None:
    if not condition:
        raise EvidenceError("public_metrics_inconsistent")


def _digest(value: JsonNode) -> bool:
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def _validate_label(value: JsonNode, vocabulary: Set[str]) -> None:
    if value is None:
        return
    _require(isinstance(value, dict) and set(value) == {"value", "sha256", "redacted"})
    _require(_digest(value["sha256"]) and type(value["redacted"]) is bool)
    if value["redacted"]:
        _require(value["value"] is None)
    else:
        _require(isinstance(value["value"], str) and is_reviewed(value["value"], vocabulary))
        _require(value["sha256"] == fingerprint(value["value"]))


def _validate_span_numbers(row: JsonObject) -> None:
    for key in (*NUMERIC, "sampled_span_ns"):
        value = row[key]
        _require(value is None or type(value) in (float, int) and math.isfinite(value))


def _validate_counts(row: JsonObject) -> None:
    for key in ("sample_count", "excluded"):
        _require(type(row[key]) is int and row[key] >= 0)
    for key in ("numerator", "denominator", "sampled_span_ns"):
        _require(row[key] is None or type(row[key]) is int and row[key] >= 0)
    _require(row["coverage_ratio"] is None or 0 <= row["coverage_ratio"] <= 1)


def _validate_row_shape(row: JsonObject, index: int, keys: Set[str]) -> None:
    _require(isinstance(row, dict) and set(row) == keys)
    _require(type(row["observation_index"]) is int and row["observation_index"] == index)
    _validate_span_numbers(row)
    _validate_counts(row)


def _validate_row_labels(row: JsonObject, vocabulary: Set[str]) -> None:
    for key in TEXT:
        _validate_label(row[key], vocabulary)
        if key != "missing_reason":
            _require(row[key] is not None)
    _require(row["status"]["value"] in ("observed", "derived", "missing", "not_applicable"))
    missing = row["status"]["value"] in ("missing", "not_applicable")
    _require(missing == (row["value"] is None))
    _require(missing == (row["missing_reason"] is not None))


def _validate_row_identity(row: JsonObject, vocabulary: Set[str]) -> None:
    _require(type(row["source_comparison_eligible"]) is bool)
    _require(row["public_comparison_authorized"] is False)
    missing = row["status"]["value"] in ("missing", "not_applicable")
    _require(not missing or not row["source_comparison_eligible"])
    _require(row["request_identity_sha256"] is None or _digest(row["request_identity_sha256"]))
    group = row["group"]
    _require(
        isinstance(group, dict)
        and set(group) == {"workload_identity_sha256", "category", "error_category"}
    )
    _require(
        group["workload_identity_sha256"] is None
        or _digest(group["workload_identity_sha256"])
    )
    _validate_label(group["category"], vocabulary)
    _validate_label(group["error_category"], vocabulary)


def _validate_row(row: JsonObject, index: int, keys: Set[str], vocabulary: Set[str]) -> None:
    _validate_row_shape(row, index, keys)
    _validate_row_labels(row, vocabulary)
    _validate_row_identity(row, vocabulary)
    _require(isinstance(row["limitations"], list))
    for value in row["limitations"]:
        _require(value is not None)
        _validate_label(value, vocabulary)


def validate_metrics(metrics: JsonObject) -> None:
    vocabulary = VOCABULARY | {
        m[key]
        for m in catalogue("metrics")["items"]
        for key in ("metric_id", "unit", "layer", "source")
    }

    _require(isinstance(metrics, dict))
    _require(metrics.get("projection_version") == "public-metrics.v1")
    rows = metrics.get("observations")
    _require(isinstance(rows, list))
    _require(type(metrics.get("observation_count")) is int)
    _require(metrics["observation_count"] == len(rows))
    keys = (
        set(NUMERIC)
        | set(TEXT)
        | {
            "observation_index",
            "request_identity_sha256",
            "group",
            "limitations",
            "source_comparison_eligible",
            "public_comparison_authorized",
            "sampled_span_ns",
        }
    )
    for index, row in enumerate(rows):
        _validate_row(row, index, keys, vocabulary)
