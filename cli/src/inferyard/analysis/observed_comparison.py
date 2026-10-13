"""Descriptive comparisons with task, denominator and per-metric boundaries."""

from inferyard.analysis.comparison import quality_differences
from inferyard.evidence.storage import json_bytes


def _tasks(data):
    selected = data["selection"]["case_ids"]
    cases = {case["case_id"]: case for case in data["bundle"]["cases"]}
    return {
        "cases": [cases[key] for key in selected],
        "answer_policy": data["bundle"]["answer_policy"],
        "task_protocol": data["bundle"]["task_protocol"],
    }


def _paired_requests(left, right):
    def index(data):
        counts, rows = {}, {}
        for request in data.get("requests", []):
            case = request["case_id"]
            ordinal = counts.get(case, 0)
            counts[case] = ordinal + 1
            rows[case, ordinal] = request
        return rows

    a, b = index(left), index(right)
    rows = []
    for case, ordinal in sorted(a.keys() | b.keys()):
        pair = [index.get((case, ordinal), {}) for index in (a, b)]
        rows.append(
            {
                "case_id": case,
                "occurrence": ordinal,
                "left": pair[0].get("score"),
                "right": pair[1].get("score"),
                "execution_states": [r.get("execution_state") for r in pair],
            }
        )
    return rows


def observed_differences(left, right, comparison):
    pair = (left, right)
    task_match = json_bytes(_tasks(left)) == json_bytes(_tasks(right))
    counts = [data["summary"]["counts"] for data in pair]
    denominator_match = all(
        counts[0].get(key) == counts[1].get(key) for key in ("planned", "valid_executed")
    )
    definitions = [d["run"]["definition_versions"] for d in pair]
    measurement = definitions[0]["measurement"] == definitions[1]["measurement"]
    scoring = (
        definitions[0]["scoring"] == definitions[1]["scoring"]
        and left["selection"]["scorer_sha256"] == right["selection"]["scorer_sha256"]
    )
    if comparison.get("definition") == "phase2.v3" and any(
        "implementation_identity" in d["run"] for d in pair
    ):
        scoped = {c["field"]: c["status"] == "same" for c in comparison["conditions"]}
        measurement = measurement and scoped.get("run.implementation_identity.measurement", False)
        scoring = scoring and scoped.get("run.implementation_identity.scoring", False)
    protocol_match = all(
        c["status"] == "same" for c in comparison["conditions"] if c["field"] == "task_protocol"
    )
    complete = all(data["summary"]["completeness"] == "complete" for data in pair)
    completion = [d["summary"]["completion_rate"] for d in pair]
    completion_allowed = (
        task_match
        and protocol_match
        and measurement
        and denominator_match
        and all(c["value"] is not None for c in completion)
    )
    quality_allowed = task_match and protocol_match and scoring and denominator_match and complete
    reasons = [
        name
        for name, matched in (
            ("task_content_mismatch", task_match),
            ("task_protocol_mismatch", protocol_match),
            ("denominator_mismatch", denominator_match),
            ("measurement_definition_mismatch", measurement),
            ("scoring_identity_mismatch", scoring),
            ("quality_incomplete", complete),
        )
        if not matched
    ]
    return {
        "scope": "observed_samples_only_no_causal_attribution",
        "sample_scope": [
            {
                "run_id": d["run"]["run_id"],
                "case_ids": d["selection"]["case_ids"],
                "counts": d["summary"]["counts"],
                "completeness": d["summary"]["completeness"],
            }
            for d in pair
        ],
        "condition_differences": [c for c in comparison["conditions"] if c["status"] != "same"],
        "completion_rate": {
            "left": completion[0]["value"],
            "right": completion[1]["value"],
            "difference": completion[1]["value"] - completion[0]["value"]
            if completion_allowed
            else None,
        },
        "quality": quality_differences(left, right, quality_allowed),
        "per_case": _paired_requests(left, right),
        "performance": _performance(left, right, comparison),
        "reasons": reasons,
        "performance_requires_existing_metric_qualification": True,
    }


def _performance(left, right, comparison):
    """Show paired values without manufacturing a performance qualification."""
    if "performance_analysis" in comparison:
        return comparison["performance_analysis"]["differences"]
    from inferyard.analysis.performance_comparison import finite, metric_index

    indices = [metric_index(data) for data in (left, right)]
    rows = []
    for key in sorted(indices[0].keys() | indices[1].keys(), key=str):
        groups = [index.get(key, []) for index in indices]
        if any(len(group) != 1 for group in groups):
            continue
        a, b = (group[0] for group in groups)
        if any(a[field] != b[field] for field in ("definition_version", "source", "layer", "unit")):
            continue
        code, statistic, category, case, source = key
        rows.append(
            {
                "metric_id": code,
                "statistic": statistic,
                "category": category,
                "case_id": case,
                "source": source,
                "unit": a["unit"],
                "left": a["value"] if finite(a["value"]) else None,
                "right": b["value"] if finite(b["value"]) else None,
                "difference": None,
                "eligible": False,
                "reasons": ["performance_evidence_not_supplied"],
                "left_limits": a.get("limitations", []),
                "right_limits": b.get("limitations", []),
                "sample_counts": [a.get("sample_count"), b.get("sample_count")],
            }
        )
    return rows
