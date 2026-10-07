"""Actual input length and position cohorts; declarations do not imply observations."""

from inferyard.contracts.schemas_common import (
    EXECUTION,
    HASH,
    IDENTIFIER,
    LABEL,
    NAT,
    NUMBER,
    POS,
    STRING_LIST,
    array,
    enum,
    nullable,
    obj,
)
from inferyard.contracts.schemas_summary_metrics import (
    CATEGORIES,
    COUNTS,
    FRACTION,
    NONNEGATIVE,
)

LENGTH_RESOURCES = obj(
    {
        "series": array(
            obj(
                {
                    **{
                        key: LABEL
                        for key in (
                            "metric_id",
                            "definition_version",
                            "statistic",
                            "unit",
                            "layer",
                            "source",
                        )
                    },
                    **{
                        key: NAT
                        for key in ("valid_executed", "observed_requests", "missing_requests")
                    },
                    "missing_reasons": COUNTS,
                    **{key: nullable(NUMBER) for key in ("min", "p50", "max")},
                    "limitations": STRING_LIST,
                }
            )
        ),
        "status": enum("available", "missing"),
        "missing_reason": nullable(LABEL),
    }
)
LENGTH_OUTCOMES = obj(
    {
        "terminal_counts": obj({key: NAT for key in EXECUTION["enum"]}),
        "failure_reasons": COUNTS,
        "excluded_reasons": array(
            obj(
                {
                    "state": enum("invalid", "cancelled", "not_executed"),
                    "reason": LABEL,
                    "count": NAT,
                }
            )
        ),
        "http_status_counts": COUNTS,
        "http_status_unknown_requests": NAT,
        "observed_http_4xx_requests": NAT,
        "rejected_requests": nullable(NAT),
        "rejection_missing_reason": nullable(LABEL),
        "rejection_definition": enum("http_4xx_response_not_context_capacity_rejection"),
        "limitations": STRING_LIST,
    }
)
INPUT_LENGTHS = obj(
    {
        "bins": array(
            obj(
                {
                    "category": enum(*CATEGORIES),
                    "actual_input_tokens": nullable(NAT),
                    "input_target_tokens": nullable(POS),
                    "output_budget_tokens": POS,
                    "planned": nullable(NAT),
                    **{
                        key: NAT
                        for key in (
                            "attempts",
                            "valid_executed",
                            "completed",
                            "failed",
                            "excluded",
                            "unscorable_completed",
                        )
                    },
                    "completion_rate": nullable(FRACTION),
                    "quality_rate": nullable(FRACTION),
                    "completed_latency_p50_ms": nullable(NONNEGATIVE),
                    "request_ids": array(nullable(IDENTIFIER)),
                    "case_template_hashes": {
                        "type": "object",
                        "additionalProperties": nullable(HASH),
                    },
                    "limitations": STRING_LIST,
                    "resources": LENGTH_RESOURCES,
                    "outcomes": LENGTH_OUTCOMES,
                }
            )
        ),
        "limitations": STRING_LIST,
    }
)
TARGET_CHECK = obj(
    {
        "status": enum("not_requested", "matched", "mismatch", "unverified"),
        "target_tokens": nullable(POS),
        "tolerance_tokens": NAT,
        "cases": array(
            obj(
                {
                    "case_id": IDENTIFIER,
                    "actual_input_tokens": nullable(NAT),
                    "status": enum("matched", "mismatch", "unverified"),
                }
            )
        ),
    }
)
POSITIONS = obj(
    {
        "family_count": NAT,
        "variant_count": NAT,
        "variants": array(
            obj(
                {
                    "case_id": IDENTIFIER,
                    "family_id": IDENTIFIER,
                    "prompt_sha256": HASH,
                    "actual_input_tokens": nullable(NAT),
                    "position_fractions": array(FRACTION, 1),
                    "positions": array(enum("front", "middle", "back"), 1),
                }
            )
        ),
        "bins": array(
            obj(
                {
                    "family_id": IDENTIFIER,
                    "task_kind": enum("retrieval", "dual_evidence"),
                    "actual_input_tokens": nullable(NAT),
                    "positions": array(enum("front", "middle", "back"), 1),
                    "category": enum(*CATEGORIES),
                    "correct": NAT,
                    "valid_executed": NAT,
                    "excluded": NAT,
                    "value": nullable(FRACTION),
                }
            )
        ),
        "limitations": STRING_LIST,
    }
)
