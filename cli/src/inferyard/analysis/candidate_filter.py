"""Threshold filtering of measured candidates; unknown evidence never passes."""

import math

from jsonschema import Draft202012Validator

from inferyard import SCHEMA_VERSION
from inferyard.analysis.comparison import compare_trials
from inferyard.application.types import CommandResult
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes, read_json
from inferyard.reporting.comparison_report import comparison_input, read_verified_comparison
from inferyard.reporting.report_common import _new_output

SELECTOR_KEYS = (
    "metric_id",
    "statistic",
    "category",
    "error_category",
    "source",
    "unit",
    "definition_version",
)
CONSTRAINT = {
    "type": "object",
    "additionalProperties": False,
    "required": [*SELECTOR_KEYS, "operator", "threshold"],
    "properties": {
        **{key: {"type": "string", "minLength": 1} for key in SELECTOR_KEYS},
        "category": {"type": ["string", "null"]},
        "error_category": {"type": ["string", "null"]},
        "operator": {"enum": ["<=", ">="]},
        "threshold": {"type": "number"},
        "aggregation": {"enum": ["all_requests"]},
    },
}
SPEC_SCHEMA = {
    "type": "object",
    "additionalProperties": False,
    "required": ["version", "reference", "candidates", "constraints"],
    "properties": {
        "version": {"const": 1},
        "reference": {"type": "string", "minLength": 1},
        "candidates": {
            "type": "array",
            "maxItems": 1000,
            "uniqueItems": True,
            "items": {"type": "string", "minLength": 1},
        },
        "constraints": {"type": "array", "minItems": 1, "maxItems": 100, "items": CONSTRAINT},
        "comparisons": {
            "type": "object",
            "additionalProperties": {"type": "string", "minLength": 1},
        },
    },
}


def finite_number(value):
    try:
        return type(value) in (int, float) and math.isfinite(value)
    except OverflowError:
        return False


def validate_spec(spec):
    if not Draft202012Validator(SPEC_SCHEMA).is_valid(spec):
        raise EvidenceError("candidate_filter_spec_invalid")
    if any(not finite_number(c["threshold"]) for c in spec["constraints"]):
        raise EvidenceError("candidate_filter_threshold_not_finite")
    if set(spec.get("comparisons", {})) - set(spec["candidates"]):
        raise EvidenceError("comparison_for_unknown_candidate")
    if any(
        c.get("aggregation") == "all_requests"
        and (
            c["metric_id"] not in {f"C0{i}" for i in range(1, 9)} or c["error_category"] is not None
        )
        for c in spec["constraints"]
    ):
        raise EvidenceError("all_requests_requires_resource_metric_without_error_category")


def contextual_metric_eligible(metric, comparison, *, case_id=None):
    """A pair-wide permission never grants permission to every metric."""
    analysis = comparison.get("performance_analysis", {})
    if not analysis.get("prerequisites_eligible") or analysis.get("blockers"):
        return False
    if metric["group"].get("error_category") is not None:
        return False
    rows = [
        row
        for row in analysis.get("differences", [])
        if row.get("case_id") == case_id
        and row.get("category") == metric["group"].get("category")
        and all(
            row.get(key) == metric.get(key) for key in ("metric_id", "statistic", "source", "unit")
        )
    ]
    return (
        len(rows) == 1
        and rows[0].get("eligible") is True
        and rows[0].get("right") == metric["value"]
    )


def evaluate_candidate(data, comparison, constraints):
    """Use an exact trial-level observation; never pool samples or choose duplicates."""
    rows = []
    for constraint in constraints:
        if constraint.get("aggregation") == "all_requests":
            from inferyard.analysis.resource_constraints import evaluate

            rows.append(evaluate(data, comparison, constraint, contextual_metric_eligible))
            continue
        matches = [
            metric
            for metric in data["summary"].get("metric_observations", [])
            if metric["request_id"] is None
            and all(
                (
                    metric["group"].get(key)
                    if key in ("category", "error_category")
                    else metric.get(key)
                )
                == constraint[key]
                for key in SELECTOR_KEYS
            )
        ]
        metric = matches[0] if len(matches) == 1 else None
        value = metric["value"] if metric else None
        kind = (
            "quality"
            if constraint["metric_id"].startswith("Q")
            else "completion"
            if constraint["metric_id"].startswith("R")
            else "performance"
        )
        reason = None
        if data["summary"]["completeness"] != "complete":
            reason = "incomplete_trial"
        elif len(matches) != 1:
            reason = "metric_missing" if not matches else "ambiguous_metric"
        elif not finite_number(value):
            reason = "metric_value_unknown"
        elif (
            kind == "performance"
            and "performance_analysis" in comparison
            and not contextual_metric_eligible(metric, comparison)
        ):
            reason = "metric_not_comparable"
        elif metric.get("comparison_eligible") is not True and not (
            kind == "performance" and contextual_metric_eligible(metric, comparison)
        ):
            reason = "metric_not_comparable"
        elif not comparison["eligibility"][kind]:
            reason = "candidate_not_comparable_to_reference"
        passed = reason is None and (
            value <= constraint["threshold"]
            if constraint["operator"] == "<="
            else value >= constraint["threshold"]
        )
        rows.append(
            {
                "constraint": constraint,
                "value": value,
                "status": "unknown" if reason else "pass" if passed else "fail",
                "reason": reason,
            }
        )
    matched = bool(rows) and all(row["status"] == "pass" for row in rows)
    return {
        "matched": matched,
        "constraints": rows,
        "comparison": comparison,
        "status": "pass"
        if matched
        else "fail"
        if any(row["status"] == "fail" for row in rows)
        else "unknown",
    }


def filter_candidates(spec, base):
    validate_spec(spec)
    reference, reference_ref = comparison_input((base / spec["reference"]).resolve())
    candidates, seen = [], set()
    for path in spec["candidates"]:
        data, ref = comparison_input((base / path).resolve())
        if ref["run_id"] in seen:
            raise EvidenceError("duplicate_candidate_run")
        seen.add(ref["run_id"])
        comparison = compare_trials(reference, data)
        linked = spec.get("comparisons", {}).get(path)
        if linked is not None:
            from inferyard.evidence.source_locations import comparison_locations, resolve_source

            comparison_root = (base / linked).resolve()
            comparison = read_verified_comparison(comparison_root)
            for source in comparison_locations(comparison):
                source["path"] = str(resolve_source(comparison_root, source["path"]))
            if comparison["source_runs"] != [reference_ref, ref]:
                raise EvidenceError("candidate_comparison_sources_or_order_mismatch")
        candidates.append(
            {"source": ref, **evaluate_candidate(data, comparison, spec["constraints"])}
        )
    resource_scope = any(c.get("aggregation") == "all_requests" for c in spec["constraints"])
    return {
        "schema_version": SCHEMA_VERSION,
        "format_version": 1,
        "spec": spec,
        "reference": reference_ref,
        "candidates": candidates,
        "matched_run_ids": [row["source"]["run_id"] for row in candidates if row["matched"]],
        "limitations": [
            "measured_candidates_only",
            "no_automatic_ranking_or_recommendation",
            "thresholds_are_user_criteria_not_pre_registered_acceptance",
            *(
                [
                    "all_request_resource_constraints_apply_only_to_qualified_observed_windows_not_continuous_limits"
                ]
                if resource_scope
                else []
            ),
        ],
    }


def execute(request):
    spec = read_json(request.filter_spec)
    result = filter_candidates(spec, request.filter_spec.parent)
    protected = [request.filter_spec.parent / p for p in [spec["reference"], *spec["candidates"]]]
    protected.extend(request.filter_spec.parent / p for p in spec.get("comparisons", {}).values())
    for candidate in result["candidates"]:
        protected.extend(
            request.filter_spec.parent / proof["source"]["path"]
            for proof in candidate["comparison"].get("performance_evidence", [])
        )
    _new_output(request.out, protected)
    atomic_bytes(request.out / "candidates.json", json_bytes(result))
    return 0, CommandResult(
        request.command,
        "filtered",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )
