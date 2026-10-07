"""Validate public observation structure without claiming source authenticity."""

import math
import re

from inferyard.analysis.public_metrics import NUMERIC, TEXT, VOCABULARY, fingerprint
from inferyard.evidence.storage import EvidenceError
from inferyard.registry import catalogue


def validate_metrics(metrics):
    def require(condition):
        if not condition:
            raise EvidenceError("public_metrics_inconsistent")

    def digest(value):
        return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None

    vocabulary = VOCABULARY | {
        m[key]
        for m in catalogue("metrics")["items"]
        for key in ("metric_id", "unit", "layer", "source")
    }

    def label(value):
        if value is None:
            return
        require(isinstance(value, dict) and set(value) == {"value", "sha256", "redacted"})
        require(digest(value["sha256"]) and type(value["redacted"]) is bool)
        if value["redacted"]:
            require(value["value"] is None)
        else:
            require(isinstance(value["value"], str) and value["value"] in vocabulary)
            require(value["sha256"] == fingerprint(value["value"]))

    require(isinstance(metrics, dict))
    require(metrics.get("projection_version") == "public-metrics.v1")
    rows = metrics.get("observations")
    require(isinstance(rows, list))
    require(type(metrics.get("observation_count")) is int)
    require(metrics["observation_count"] == len(rows))
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
        require(isinstance(row, dict) and set(row) == keys)
        require(type(row["observation_index"]) is int and row["observation_index"] == index)
        for key in (*NUMERIC, "sampled_span_ns"):
            value = row[key]
            require(value is None or type(value) in (float, int) and math.isfinite(value))
        for key in ("sample_count", "excluded"):
            require(type(row[key]) is int and row[key] >= 0)
        for key in ("numerator", "denominator", "sampled_span_ns"):
            require(row[key] is None or type(row[key]) is int and row[key] >= 0)
        require(row["coverage_ratio"] is None or 0 <= row["coverage_ratio"] <= 1)
        for key in TEXT:
            label(row[key])
            if key != "missing_reason":
                require(row[key] is not None)
        require(row["status"]["value"] in ("observed", "derived", "missing", "not_applicable"))
        missing = row["status"]["value"] in ("missing", "not_applicable")
        require(missing == (row["value"] is None))
        require(missing == (row["missing_reason"] is not None))
        require(type(row["source_comparison_eligible"]) is bool)
        require(row["public_comparison_authorized"] is False)
        require(not missing or not row["source_comparison_eligible"])
        require(row["request_identity_sha256"] is None or digest(row["request_identity_sha256"]))
        group = row["group"]
        require(
            isinstance(group, dict)
            and set(group) == {"workload_identity_sha256", "category", "error_category"}
        )
        require(
            group["workload_identity_sha256"] is None or digest(group["workload_identity_sha256"])
        )
        label(group["category"])
        label(group["error_category"])
        require(isinstance(row["limitations"], list))
        for value in row["limitations"]:
            require(value is not None)
            label(value)
