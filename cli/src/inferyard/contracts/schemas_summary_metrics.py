"""Strict quality, performance, resource and duration summary components."""

from inferyard.contracts.schemas_common import (
    BOOL,
    EXECUTION,
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

CATEGORIES = (
    "instruction",
    "extraction",
    "qa",
    "math",
    "classification",
    "structured",
    "performance",
    "svg",
)
FRACTION = {"type": "number", "minimum": 0, "maximum": 1}
NONNEGATIVE = {"type": "number", "minimum": 0}
COUNTS = {"type": "object", "additionalProperties": NAT}
INTERVAL = {**array(NAT, 2), "maxItems": 2}
RATE = obj(
    {
        "numerator": NAT,
        "denominator": NAT,
        "excluded": NAT,
        "value": nullable(FRACTION),
        "reason": nullable(LABEL),
    }
)
DISTRIBUTION = obj(
    {
        "sample_count": NAT,
        "excluded": NAT,
        **{key: nullable(NUMBER) for key in ("min", "p50", "p95", "max")},
        "quantile_method": enum("nearest_rank.phase2.v1"),
        "p95_reason": nullable(LABEL),
        "p95_exploratory": BOOL,
    }
)
QUALITY_GROUP = obj(
    {
        **{key: NAT for key in ("valid", "passed", "unscorable", "excluded")},
        "rate": RATE,
    }
)
QUALITY = obj(
    {
        "Q01": obj({c: QUALITY_GROUP for c in CATEGORIES if c not in ("performance", "svg")}, []),
        **{key: RATE for key in ("Q02", "Q03", "Q04", "Q05", "Q06", "Q07")},
        "Q08": obj(
            {
                "value": nullable(FRACTION),
                "reason": nullable(LABEL),
                "sample_count": NAT,
                "classes": array(
                    obj(
                        {
                            "label": LABEL,
                            **{key: NAT for key in ("support", "tp", "fp", "fn")},
                            "f1": FRACTION,
                        }
                    )
                ),
                "zero_division": {"type": "integer", "const": 0},
                "confusion": array(
                    obj({"expected": LABEL, "predicted": nullable(LABEL), "count": POS})
                ),
            }
        ),
    }
)
DURATION_QUALITY = obj(
    {
        "status": enum("repeated_probe_observations_only"),
        "independent_cases": POS,
    }
)
TIMING_POINT = obj({"value": nullable(NONNEGATIVE), "unit": enum("ms"), "reason": nullable(LABEL)})
PERFORMANCE_GROUP = obj(
    {
        "metrics": obj(
            {
                **{
                    code: obj(
                        {
                            **DISTRIBUTION["properties"],
                            "unit": enum("token/s" if code in ("L04", "L06", "L07") else "ms"),
                            "missing_reasons": COUNTS,
                        }
                    )
                    for code in ("L01", "L02", "L03", "L04", "L06", "L07")
                },
                "L05": array(
                    obj(
                        {
                            "request_id": IDENTIFIER,
                            "unit": enum("ms"),
                            "reason": nullable(LABEL),
                            "intervals": array(NONNEGATIVE),
                            "distribution": DISTRIBUTION,
                            "source": nullable(enum("decoded_delta")),
                        }
                    )
                ),
            }
        ),
        **{key: NAT for key in ("planned", "completed", "failed", "timeout_count")},
        "failure_categories": COUNTS,
        "failed_first_events": array(
            obj(
                {
                    "request_id": IDENTIFIER,
                    "L01": TIMING_POINT,
                    "L02": TIMING_POINT,
                }
            )
        ),
    }
)
PERFORMANCE = obj({category: PERFORMANCE_GROUP for category in CATEGORIES}, [])
RESOURCE_POINT = obj(
    {
        "value": nullable(NUMBER),
        "sample_count": NAT,
        "excluded": NAT,
        "reason": nullable(LABEL),
        "limitations": STRING_LIST,
        "interval": nullable(INTERVAL),
        "coverage": nullable(FRACTION),
    },
    ["value", "sample_count", "excluded", "reason", "limitations"],
)
COUNTER_POINT = obj(
    {
        **RESOURCE_POINT["properties"],
        "scale": nullable(POS),
        "largest_sample_gap_ns": nullable(NAT),
    }
)
RESOURCES = obj(
    {
        "baseline_rss_bytes": nullable(NONNEGATIVE),
        "requests": array(
            obj(
                {
                    "request_id": IDENTIFIER,
                    "category": enum(*CATEGORIES),
                    "metrics": obj(
                        {
                            **{key: RESOURCE_POINT for key in ("C01", "C02", "C03")},
                            **{key: COUNTER_POINT for key in ("C04", "C05")},
                            "C06": obj(
                                {
                                    key: COUNTER_POINT
                                    for key in ("system_swap_in", "system_swap_out")
                                }
                            ),
                        }
                    ),
                }
            )
        ),
    }
)
DURATION_CASE = obj(
    {
        "case_id": IDENTIFIER,
        **{
            key: NAT
            for key in (
                "started",
                "completed_in_send_cohort",
                "unfinished_or_invalid_timing",
                "cross_boundary_completed",
                "failed",
                "valid_executed",
            )
        },
        "execution_states": obj({key: NAT for key in EXECUTION["enum"]}, []),
        "median_latency_ns": nullable(NONNEGATIVE),
        "baseline_latency_ns": nullable(NONNEGATIVE),
        "latency_drift_ratio": nullable(NUMBER),
        "latency_difference_ns": nullable(NUMBER),
        "missing_reason": nullable(LABEL),
    }
)
DURATION_WINDOW = obj(
    {
        "index": NAT,
        "start_ns": NAT,
        "end_ns": NAT,
        "full_width": BOOL,
        "cases": array(DURATION_CASE, 1),
    }
)
DURATION = {
    "oneOf": [
        obj(
            {
                "window_completed": {"type": "boolean", "const": False},
                "reason": enum("window_not_started"),
                "windows": {**array(DURATION_WINDOW), "maxItems": 0},
            }
        ),
        obj(
            {
                "start_ns": NAT,
                "admission_deadline_ns": NAT,
                "drain_deadline_ns": NAT,
                "request_limit": POS,
                "admitted_requests": NAT,
                "window_completed": BOOL,
                "reason": enum(
                    "window_close_missing",
                    "duration_elapsed",
                    "request_limit_reached",
                    "drain_deadline_exceeded",
                    "interrupted",
                ),
                "windows": array(DURATION_WINDOW, 1),
                "independent_cases": POS,
                "limitations": STRING_LIST,
                "probe_coverage_complete": BOOL,
                "closed_ns": NAT,
            },
            [
                "start_ns",
                "admission_deadline_ns",
                "drain_deadline_ns",
                "request_limit",
                "admitted_requests",
                "window_completed",
                "reason",
                "windows",
                "independent_cases",
                "limitations",
                "probe_coverage_complete",
            ],
        ),
    ]
}
IDLE_RSS = obj(
    {
        "points": array(
            obj(
                {
                    "cycle_index": NAT,
                    "request_id": IDENTIFIER,
                    "rss_bytes": nullable(NAT),
                    "missing_reason": nullable(LABEL),
                }
            )
        ),
        "first_cycle_rss_bytes": nullable(NAT),
    }
)
