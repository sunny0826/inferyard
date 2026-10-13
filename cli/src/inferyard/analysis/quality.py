"""Quality reductions preserve every valid failure and all expected fields."""

from collections import Counter

from inferyard.analysis.aggregate import rate
from inferyard.contracts.validation import ContractError, validate_document


def _expected_counts(case):
    if case["category"] == "instruction":
        r = case["rules"]
        return len(r["literal_contains"]) + len(r["literal_forbidden"]) + (
            r["nonempty_line_count"] is not None
        ), 0
    if case["category"] in ("extraction", "structured"):
        return 0, len(case["rules"]["fields"])
    return 0, 0


def summarize_quality(cases, requests, *, complete):
    by_id = {case["case_id"]: case for case in cases}
    if len(by_id) != len(cases):
        raise ContractError("quality.cases", "duplicate case_id")
    seen = set()
    groups, confusion, expected_labels = {}, Counter(), None
    for case in cases:
        category = case["category"]
        if category in ("performance", "svg"):
            continue
        groups.setdefault(category, {"valid": 0, "passed": 0, "unscorable": 0, "excluded": 0})
        if category == "classification":
            labels = case["rules"]["labels"]
            if expected_labels is not None and labels != expected_labels:
                raise ContractError("quality.labels", "inconsistent frozen labels")
            expected_labels = labels
    counts = Counter()
    quality_complete = complete
    for request in requests:
        if request["case_id"] in seen:
            raise ContractError("quality.requests", "duplicate case in a single trial")
        seen.add(request["case_id"])
        case = by_id.get(request["case_id"])
        if case is None:
            raise ContractError("quality.case_id", "unknown case")
        category = case["category"]
        if category in ("performance", "svg"):
            continue
        group = groups[category]
        if request["execution_state"] not in ("completed", "failed"):
            group["excluded"] += 1
            quality_complete = False
            continue
        group["valid"] += 1
        score = request.get("score") if request["execution_state"] == "completed" else None
        if score is not None:
            validate_document("score", score)
            if score["category"] != category:
                raise ContractError("quality.score", "score category differs from frozen task")
        if request["execution_state"] == "completed" and (
            score is None or score["quality_state"] == "unscorable"
        ):
            group["unscorable"] += 1
            quality_complete = False
            score = None
        if score:
            group["passed"] += score["quality_state"] == "pass"
        constraints, fields = _expected_counts(case)
        counts["constraints"] += constraints
        counts["fields"] += fields
        if score:
            if (
                len(score["constraint_results"]) != constraints
                or len(score["field_results"]) != fields
            ):
                raise ContractError("quality.score", "scored denominator differs from frozen task")
            counts["constraints_passed"] += sum(r["passed"] for r in score["constraint_results"])
            counts["fields_passed"] += sum(r["passed"] for r in score["field_results"])
        if category in ("structured", "extraction"):
            counts["structured"] += 1
            counts["parsed"] += bool(score and score["json_parse_ok"])
            counts["parse_unknown"] += bool(score and score["json_parse_ok"] is None)
            counts["schema"] += bool(score and score["schema_ok"])
        if category == "classification":
            predicted = score["predicted_label"] if score else None
            if predicted is not None and predicted not in expected_labels:
                raise ContractError("quality.predicted_label", "invalid score label")
            confusion[(case["rules"]["expected"], predicted)] += 1
    for case_id in by_id.keys() - seen:
        category = by_id[case_id]["category"]
        if category not in ("performance", "svg"):
            groups[category]["excluded"] += 1
            quality_complete = False
    reason = None if quality_complete else "incomplete_run"
    for group in groups.values():
        group["rate"] = rate(group["passed"], group["valid"], group["excluded"], reason)
    metrics = {
        "Q01": groups,
        "Q02": rate(counts["constraints_passed"], counts["constraints"], reason=reason),
        "Q03": rate(
            counts["parsed"],
            counts["structured"],
            reason=reason
            or ("legacy_json_parse_result_unavailable" if counts["parse_unknown"] else None),
        ),
        "Q04": rate(counts["schema"], counts["structured"], reason=reason),
        "Q05": rate(counts["fields_passed"], counts["fields"], reason=reason),
        "Q06": groups.get("qa", {}).get("rate", rate(0, 0)),
        "Q07": groups.get("math", {}).get("rate", rate(0, 0)),
    }
    classes = []
    for label in expected_labels or []:
        tp = confusion[(label, label)]
        fn = sum(n for (truth, pred), n in confusion.items() if truth == label and pred != label)
        fp = sum(n for (truth, pred), n in confusion.items() if truth != label and pred == label)
        den = 2 * tp + fp + fn
        classes.append(
            {
                "label": label,
                "support": tp + fn,
                "tp": tp,
                "fp": fp,
                "fn": fn,
                "f1": 2 * tp / den if den else 0.0,
            }
        )
    total = sum(confusion.values())
    metrics["Q08"] = {
        "value": sum(c["f1"] for c in classes) / len(classes)
        if classes and total and quality_complete
        else None,
        "reason": reason or (None if classes and total else "no_classification_samples"),
        "sample_count": total,
        "classes": classes,
        "zero_division": 0,
        "confusion": [
            {"expected": a, "predicted": b, "count": n} for (a, b), n in confusion.items()
        ],
    }
    return metrics
