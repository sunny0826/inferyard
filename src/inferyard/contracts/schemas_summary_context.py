"""Environment and observer qualification structures in an offline summary."""

from inferyard.contracts.schemas_common import (
    BOOL,
    EXECUTION,
    IDENTIFIER,
    LABEL,
    NAT,
    NUMBER,
    PHASE,
    STRING_LIST,
    array,
    enum,
    nullable,
    obj,
)
from inferyard.contracts.schemas_experiment import EXPERIMENT, REFERENCE
from inferyard.contracts.schemas_summary_metrics import FRACTION

ENVIRONMENT = obj(
    {
        "cpu_policies": obj(
            {"complete_and_stable": BOOL, "reasons": STRING_LIST, "snapshot_count": NAT}
        ),
        "stable_observed_environment": BOOL,
        "reasons": STRING_LIST,
        "observation_count": NAT,
        "largest_request_gap_ns": nullable(NAT),
        "limitations": STRING_LIST,
        "comparison_eligible": {"type": "boolean", "const": False},
    }
)
SCHEDULE = obj(
    {
        **{
            key: NAT
            for key in (
                "periodic_samples",
                "boundary_samples",
                "work_exceeds_interval_count",
                "boundary_work_ns",
            )
        },
        "max_work_ns": nullable(NAT),
        "max_late_ns": nullable(NAT),
        "overhead_gate": enum("not_verified"),
        "limitations": STRING_LIST,
    }
)
EXTERNAL_CPU = obj(
    {
        "source": LABEL,
        "intervals": array(
            obj(
                {
                    "start_ns": nullable(NAT),
                    "end_ns": NAT,
                    "value_percent": nullable(NUMBER),
                    "missing_reason": nullable(LABEL),
                    "phase": PHASE,
                    "request_id": nullable(IDENTIFIER),
                }
            )
        ),
        "sample_count": NAT,
        "valid_intervals": NAT,
        "observed_max_percent": nullable(NUMBER),
        "comparison_eligible": {"type": "boolean", "const": False},
        "limitations": STRING_LIST,
    }
)
EXTERNAL_REQUESTS = obj(
    {
        "policy": nullable(EXPERIMENT["properties"]["performance_environment"]),
        "requests": array(
            obj(
                {
                    "request_id": nullable(IDENTIFIER),
                    "execution_state": EXECUTION,
                    "covered_ns": NAT,
                    "duration_ns": nullable(NAT),
                    "coverage_ratio": nullable(FRACTION),
                    "observed_max_percent": nullable(NUMBER),
                    "observed_intervals": NAT,
                    "eligible": BOOL,
                    "reasons": STRING_LIST,
                }
            )
        ),
        "all_requests_eligible": BOOL,
        "limitations": STRING_LIST,
    }
)
MEASUREMENT_CONTEXT = obj(
    {
        "environment_qualification": obj(
            {
                "definition": enum("observed_environment_qualification.v1"),
                "eligible": BOOL,
                "reasons": STRING_LIST,
                "request_count": NAT,
                "limitations": STRING_LIST,
            }
        ),
        "external_cpu": EXTERNAL_CPU,
        "external_cpu_requests": EXTERNAL_REQUESTS,
        "environment": ENVIRONMENT,
        "collector_schedule": SCHEDULE,
        "evidence_refs": array(REFERENCE),
        "performance_comparison_eligible": {"type": "boolean", "const": False},
    }
)
CACHE = obj(
    {
        "definition": enum("observed_prefix_reuse_sequence.v1"),
        "rows": array(
            obj(
                {
                    "ordinal": nullable(NAT),
                    "request_id": nullable(IDENTIFIER),
                    "phase": PHASE,
                    "case_id": nullable(IDENTIFIER),
                    "execution_state": enum(*EXECUTION["enum"], "unfinished"),
                    "requested_reuse": BOOL,
                    "cached_prompt_tokens": nullable(NAT),
                    "processed_prompt_tokens": nullable(NAT),
                    "reuse_observed": nullable(BOOL),
                    "policy_consistent": nullable(BOOL),
                    "missing_reason": nullable(LABEL),
                    "source": LABEL,
                }
            )
        ),
        **{
            key: NAT
            for key in (
                "planned_formal_requests",
                "started_requests",
                "observed_requests",
                "missing_requests",
            )
        },
        "policy_consistent": nullable(BOOL),
        "initial_no_reuse_observed": nullable(BOOL),
        "comparison_eligible": {"type": "boolean", "const": False},
        "limitations": STRING_LIST,
    }
)
