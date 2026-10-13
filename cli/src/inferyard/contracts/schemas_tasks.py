"""Unified objective tasks, review proof and scoring records."""

from inferyard.contracts.schemas_common import (
    BOOL,
    HASH,
    IDENTIFIER,
    LABEL,
    NAT,
    NUMBER,
    QUALITY,
    TEXT,
    VERSION,
    array,
    enum,
    nullable,
    obj,
)

INSTRUCTION_RULES = obj(
    {
        "literal_contains": array(LABEL),
        "literal_forbidden": array(LABEL),
        "nonempty_line_count": nullable(NAT),
    }
)
# JSON scalar references are enough for v1 flat field extraction cases.
JSON_SCALAR = {"type": ["string", "integer", "number", "boolean", "null"]}
FIELD = obj(
    {
        "type": enum("string", "integer", "number", "boolean", "null"),
        "value": JSON_SCALAR,
    }
)
JSON_RULES = obj(
    {
        "fields": {"type": "object", "minProperties": 1, "additionalProperties": FIELD},
        "allow_extra_fields": BOOL,
    }
)
CASE_BASE = {
    "case_id": IDENTIFIER,
    "prompt": LABEL,
    "reference_answer": TEXT,
}
NONNEGATIVE = {"type": "number", "minimum": 0}
JSON_VALUE = {"type": ["object", "array", "string", "number", "boolean", "null"]}
QA = obj({"answers": array(LABEL, 1), "normalization": enum("strip", "nfkc_strip")})
MATH = obj(
    {
        "expected": NUMBER,
        "absolute_tolerance": NONNEGATIVE,
        "relative_tolerance": NONNEGATIVE,
        "unit": TEXT,
    }
)
CLASSIFICATION = obj({"labels": array(LABEL, 2), "expected": LABEL})
STRUCTURED = obj(
    {
        "json_schema": {"type": "object"},
        "expected": JSON_VALUE,
        "fields": array(TEXT, 1),
        "array_matching": enum("ordered"),
    }
)
PERFORMANCE = obj({"output_target_tokens": nullable(NAT)})
TASK_RULES = {
    "instruction": INSTRUCTION_RULES,
    "extraction": JSON_RULES,
    "qa": QA,
    "math": MATH,
    "classification": CLASSIFICATION,
    "structured": STRUCTURED,
    "performance": PERFORMANCE,
    "svg": obj({}),
}
CASE = {
    "oneOf": [
        obj({**CASE_BASE, "category": enum(category), "rules": rules})
        for category, rules in TASK_RULES.items()
    ]
}
BUNDLE = obj(
    {
        "schema_version": VERSION,
        "bundle_id": IDENTIFIER,
        "version": LABEL,
        "language": enum("zh-CN"),
        "license_note": LABEL,
        "review_records": array(
            obj(
                {
                    "reviewer": LABEL,
                    "reviewed_at": LABEL,
                    "content_sha256": HASH,
                    "conclusion": enum("approved", "rejected"),
                }
            )
        ),
        "answer_policy": obj(
            {
                "instruction_newlines": enum("crlf_to_lf"),
                "strip_line_edges": BOOL,
                "ignore_empty_lines": BOOL,
                "unicode_normalization": enum("none"),
                "reasoning": enum("separate_channel_excluded_content_unchanged"),
            }
        ),
        "cases": array(CASE, 1),
        "task_protocol": enum("quality", "performance"),
    }
)


BUNDLE["properties"]["case_review_records"] = array(
    obj(
        {
            "definition": enum("case-review.v1"),
            "case_id": IDENTIFIER,
            "content_sha256": HASH,
            "reviewer": LABEL,
            "reviewed_at": LABEL,
            "conclusion": enum("approved", "rejected"),
        }
    )
)


BUNDLE["properties"]["review_provenance"] = array(
    obj({"source_json": LABEL, "source_sha256": HASH, "source_content_sha256": HASH}), 1
)
SCORE = obj(
    {
        "scorer_version": LABEL,
        "scorer_sha256": HASH,
        "quality_state": QUALITY,
        "format_ok": nullable(BOOL),
        "content_ok": nullable(BOOL),
        "rule_results": array(
            obj(
                {
                    "rule": LABEL,
                    "passed": BOOL,
                    "expected": TEXT,
                    "observed": TEXT,
                    "reason": TEXT,
                }
            )
        ),
        "explanation": TEXT,
        "reason": nullable(LABEL),
    }
)
SCORE["properties"].update(
    {
        "schema_version": VERSION,
        "category": enum(
            "instruction",
            "extraction",
            "qa",
            "math",
            "classification",
            "structured",
            "performance",
            "svg",
        ),
        "scorer_id": LABEL,
        "quality_state": enum("pass", "fail", "unscorable", "not_applicable"),
        "json_parse_ok": nullable(BOOL),
        "schema_ok": nullable(BOOL),
        "field_results": array(obj({"path": TEXT, "passed": BOOL})),
        "constraint_results": array(obj({"rule": LABEL, "passed": BOOL})),
        "expected_label": nullable(LABEL),
        "predicted_label": nullable(LABEL),
    }
)
SCORE["required"] = list(SCORE["properties"])
