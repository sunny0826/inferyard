"""Instruction and flat extraction algorithms update the unified score directly."""

import json
import math

from inferyard.contracts.validation import ContractError, strict_json_loads


def _typed(value, expected):
    if expected == "number":
        return type(value) is int or (type(value) is float and math.isfinite(value))
    return (
        type(value)
        is {"string": str, "integer": int, "boolean": bool, "null": type(None)}[expected]
    )


def _display(value):
    return json.dumps(value, ensure_ascii=False, allow_nan=False, sort_keys=True)


def score_rules(case: dict, answer: str, policy: dict, result: dict) -> None:
    rules = []

    def rule(name, expected, observed, passed):
        rules.append(
            {
                "rule": name,
                "expected": _display(expected),
                "observed": _display(observed),
                "passed": bool(passed),
                "reason": "matched" if passed else "not_matched",
            }
        )

    rule("nonempty_final_answer", True, bool(answer.strip()), bool(answer.strip()))
    format_ok = None
    if case["category"] == "instruction":
        normalized = answer.replace("\r\n", "\n")
        spec = case["rules"]
        for literal in spec["literal_contains"]:
            rule("literal_contains", literal, literal in normalized, literal in normalized)
        for literal in spec["literal_forbidden"]:
            rule("literal_forbidden", literal, literal in normalized, literal not in normalized)
        if spec["nonempty_line_count"] is not None:
            lines = normalized.split("\n")
            if policy["strip_line_edges"]:
                lines = [line.strip() for line in lines]
            if policy["ignore_empty_lines"]:
                lines = [line for line in lines if line]
            rule(
                "nonempty_line_count",
                spec["nonempty_line_count"],
                len(lines),
                len(lines) == spec["nonempty_line_count"],
            )
        content_ok = all(item["passed"] for item in rules)
    else:
        spec = case["rules"]
        try:
            value = strict_json_loads(answer)
            parsed = True
            format_ok = type(value) is dict
        except ContractError:
            value = None
            parsed = False
            format_ok = False
        rule("json_object", True, format_ok, format_ok)
        content_ok = False
        if format_ok:
            keys = set(value)
            expected = set(spec["fields"])
            fields_ok = expected <= keys and (spec["allow_extra_fields"] or keys == expected)
            rule("field_set", sorted(expected), sorted(keys), fields_ok)
            types_ok = all(
                key in value and _typed(value[key], field["type"])
                for key, field in spec["fields"].items()
            )
            format_ok = bool(fields_ok and types_ok)
            for key, field in spec["fields"].items():
                type_ok = key in value and _typed(value[key], field["type"])
                rule("type:" + key, field["type"], type(value.get(key)).__name__, type_ok)
                value_ok = type_ok and value[key] == field["value"]
                rule("value:" + key, field["value"], value.get(key), value_ok)
            content_ok = all(item["passed"] for item in rules)
    passed = all(item["passed"] for item in rules)
    result.update(
        {
            "quality_state": "pass" if passed else "fail",
            "format_ok": format_ok,
            "content_ok": content_ok,
            "rule_results": rules,
            "explanation": "all_required_rules_matched" if passed else "required_rule_failed",
            "reason": None,
        }
    )
    if case["category"] == "instruction":
        result["constraint_results"] = [
            {"rule": r["rule"], "passed": r["passed"]}
            for r in rules
            if r["rule"] != "nonempty_final_answer"
        ]
    else:
        result["json_parse_ok"] = parsed
        result["schema_ok"] = format_ok
        for name, field in case["rules"]["fields"].items():
            passed = parsed and type(value) is dict and name in value
            passed = passed and _typed(value[name], field["type"]) and value[name] == field["value"]
            result["field_results"].append(
                {"path": "/" + name.replace("~", "~0").replace("/", "~1"), "passed": bool(passed)}
            )
