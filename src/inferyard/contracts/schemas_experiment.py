"""Frozen experiments, execution identity and derived observations."""

from copy import deepcopy

from inferyard.contracts.schemas_common import (
    BOOL,
    HASH,
    IDENTIFIER,
    LABEL,
    NAT,
    NUMBER,
    POS,
    STRING_LIST,
    VERSION,
    array,
    enum,
    nullable,
    obj,
)
from inferyard.contracts.schemas_config import ENVIRONMENT_ADMISSION
from inferyard.contracts.schemas_identity import IMPLEMENTATION_IDENTITY
from inferyard.contracts.schemas_lineage import LINEAGE, RECORDS

SECONDS = {"type": "number", "exclusiveMinimum": 0}
REFERENCE = obj({"path": LABEL, "sha256": HASH})
VERSIONS = obj({"measurement": LABEL, "scoring": LABEL, "comparison": LABEL})
FIXED_PROTOCOL = obj({"kind": enum("fixed"), "case_ids": array(IDENTIFIER, 1)})
DURATION_PROTOCOL = obj(
    {
        "kind": enum("duration"),
        "case_ids": array(IDENTIFIER, 1),
        "duration_seconds": SECONDS,
        "max_requests": POS,
        "window_seconds": SECONDS,
        "min_completed_per_case_per_window": POS,
        "drain_timeout_seconds": SECONDS,
    }
)
PROTOCOL = {"oneOf": [FIXED_PROTOCOL, DURATION_PROTOCOL]}
WORKLOAD = obj(
    {
        "workload_id": IDENTIFIER,
        "purpose": enum("quality", "performance", "position", "stability"),
        "config": REFERENCE,
        "bundle": REFERENCE,
        "protocol": PROTOCOL,
        "repeats": POS,
        "timeout_seconds": SECONDS,
        "overhead_budget_seconds": {"type": "number", "minimum": 0},
        "input_target_tokens": nullable(POS),
        "output_budget_tokens": POS,
    }
)
WORKLOAD["properties"]["input_tolerance_tokens"] = NAT
WORKLOAD["properties"]["output_mode"] = enum("strict_fixed_length")
WORKLOAD["properties"]["cache_protocol"] = enum("prism_prefix_reuse.v1")
WORKLOAD["properties"]["model_lineage"] = LINEAGE
WORKLOAD["properties"]["model_lineage_records"] = RECORDS
WORKLOAD["properties"]["repeat_case_orders"] = array(array(IDENTIFIER, 1), 1)
# Optional for existing quality/performance plans; required by position semantics.
WORKLOAD["properties"]["position_cases"] = array(
    obj(
        {
            "case_id": IDENTIFIER,
            "family_id": IDENTIFIER,
            "task_kind": enum("retrieval", "dual_evidence"),
            "prompt_sha256": HASH,
            "body_start": NAT,
            "body_end": POS,
            "evidence_spans": array(obj({"start": NAT, "end": POS, "sha256": HASH}), 1),
        }
    ),
    1,
)
EXECUTION = obj(
    {
        "concurrency": {"type": "integer", "const": 1},
        "automatic_retries": {"type": "integer", "const": 0},
        "order": enum("fixed", "seeded"),
        "seed": nullable(NAT),
        "service_transition": enum("operator_verified"),
    }
)
COMPARISON = obj({"mode": enum("model", "config", "side-by-side"), "factor": nullable(LABEL)})
BUDGET = obj(
    {
        "max_requests": POS,
        "max_wall_seconds": SECONDS,
        "min_disk_bytes": POS,
        "min_available_memory_bytes": POS,
    }
)
EXPERIMENT = obj(
    {
        "schema_version": VERSION,
        "experiment_id": IDENTIFIER,
        "name": LABEL,
        "definition_versions": VERSIONS,
        "execution": EXECUTION,
        "comparison": COMPARISON,
        "budget": BUDGET,
        "workloads": array(WORKLOAD, 1),
    }
)
EXPERIMENT["properties"]["environment_admission"] = ENVIRONMENT_ADMISSION
EXPERIMENT["properties"]["performance_environment"] = obj(
    {
        "max_external_cpu_percent": {"type": "number", "minimum": 0, "maximum": 100},
        "max_external_interval_seconds": {"type": "number", "minimum": 0.1, "maximum": 60},
    }
)
EXPERIMENT["properties"]["capacity_stop"] = enum("first_failed_request")
EXPERIMENT["properties"]["resource_comparison"] = obj(
    {
        "min_coverage_ratio": {"type": "number", "exclusiveMinimum": 0, "maximum": 1},
        "max_window_offset_difference": {"type": "number", "minimum": 0, "maximum": 1},
        "max_sample_gap_intervals": {"type": "number", "minimum": 1, "maximum": 10},
        "min_baseline_samples": {"type": "integer", "minimum": 2},
    }
)
EXPERIMENT["properties"]["safety"] = obj(
    {
        "interval_seconds": {"type": "number", "minimum": 0.1, "maximum": 60},
        "max_temperature_celsius": nullable({"type": "number", "minimum": 1, "maximum": 150}),
        "require_temperature": BOOL,
        "max_external_cpu_percent": nullable({"type": "number", "minimum": 0, "maximum": 100}),
        "check_environment": BOOL,
    }
)
EXPERIMENT["properties"]["safety"]["properties"]["intel_pstate_no_turbo"] = {
    "type": "integer",
    "enum": [0, 1],
}
TRIAL = obj(
    {
        "trial_id": IDENTIFIER,
        "workload_id": IDENTIFIER,
        "repeat_index": NAT,
        "case_order": array(IDENTIFIER, 1),
        "request_limit": POS,
        "request_budget_seconds": SECONDS,
        "total_budget_seconds": SECONDS,
        "requires_service_handoff": BOOL,
    }
)
PLAN = obj(
    {
        "schema_version": VERSION,
        "plan_sha256": HASH,
        "experiment": EXPERIMENT,
        "runtime_bindings": array(
            obj(
                {
                    "workload_id": IDENTIFIER,
                    "source_directory": LABEL,
                    "config": REFERENCE,
                }
            )
        ),
        "trials": array(TRIAL, 1),
        "request_limit": POS,
        "request_budget_seconds": SECONDS,
        "total_budget_seconds": SECONDS,
        "limitations": STRING_LIST,
    }
)
RUN = obj(
    {
        "schema_version": VERSION,
        "run_id": IDENTIFIER,
        "kind": enum("check", "run"),
        "execution_mode": enum("single", "experiment"),
        "origin": enum("measured", "migrated"),
        "experiment_id": IDENTIFIER,
        "trial_id": IDENTIFIER,
        "plan_sha256": HASH,
        "parent_run_id": nullable(IDENTIFIER),
        "relation": enum("initial", "repeat", "resume", "rerun", "derived"),
        "tool_version": LABEL,
        "tool_source_sha256": HASH,
        "definition_versions": VERSIONS,
        "resumed_case_ids": array(IDENTIFIER),
        "diagnostic": BOOL,
    }
)

RUN = {
    "oneOf": [
        RUN,  # Original schema3 records keep their saved whole-source identity.
        obj(
            {**RUN["properties"], "implementation_identity": IMPLEMENTATION_IDENTITY},
            [key for key in RUN["required"] if key != "tool_source_sha256"]
            + ["implementation_identity"],
        ),
    ]
}

METRIC_DEFINITION = obj(
    {
        "schema_version": VERSION,
        "metric_id": IDENTIFIER,
        "name": LABEL,
        "definition_version": LABEL,
        "unit": LABEL,
        "layer": enum("request", "process", "device", "engine", "host", "experiment"),
        "source": LABEL,
        "window": LABEL,
        "applicability": LABEL,
        "required_capabilities": array(IDENTIFIER),
        "aggregation": LABEL,
        "comparison_policy": LABEL,
        "implementation_status": enum("planned", "implemented", "verified"),
        "scope": enum("core", "conditional"),
        "evidence_refs": array(REFERENCE),
    }
)
METRIC_OBSERVATION = obj(
    {
        "schema_version": VERSION,
        "metric_id": IDENTIFIER,
        "definition_version": LABEL,
        "run_id": IDENTIFIER,
        "trial_id": IDENTIFIER,
        "request_id": nullable(IDENTIFIER),
        "group": obj(
            {
                "workload_id": nullable(IDENTIFIER),
                "category": nullable(LABEL),
                "error_category": nullable(LABEL),
            }
        ),
        "statistic": LABEL,
        "value": nullable(NUMBER),
        "unit": LABEL,
        "layer": METRIC_DEFINITION["properties"]["layer"],
        "source": LABEL,
        "status": enum("observed", "derived", "missing", "not_applicable"),
        "missing_reason": nullable(LABEL),
        "sample_count": NAT,
        "numerator": nullable(NAT),
        "denominator": nullable(NAT),
        "excluded": NAT,
        "sampled_start_ns": nullable(NAT),
        "sampled_end_ns": nullable(NAT),
        "coverage_ratio": nullable({"type": "number", "minimum": 0, "maximum": 1}),
        "comparison_eligible": BOOL,
        "limitations": STRING_LIST,
        "evidence_refs": array(REFERENCE),
    }
)
AGGREGATE_OBSERVATION = deepcopy(METRIC_OBSERVATION)
AGGREGATE_OBSERVATION["properties"].update(
    {
        "run_id": {"type": "null"},
        "trial_id": {"type": "null"},
        "request_id": {"type": "null"},
        "analysis_id": IDENTIFIER,
        "source_run_ids": array(IDENTIFIER, 1),
        "sampled_start_ns": {"type": "null"},
        "sampled_end_ns": {"type": "null"},
        "coverage_ratio": {"type": "null"},
    }
)
AGGREGATE_OBSERVATION["required"] = list(AGGREGATE_OBSERVATION["properties"])
METRIC_OBSERVATION = {"oneOf": [METRIC_OBSERVATION, AGGREGATE_OBSERVATION]}
ANALYSIS = obj(
    {
        "schema_version": VERSION,
        "analysis_id": IDENTIFIER,
        "source_runs": array(obj({"run_id": IDENTIFIER, "manifest_sha256": HASH}), 1),
        "parent_analysis_id": nullable(IDENTIFIER),
        "reason": LABEL,
        "definition_versions": VERSIONS,
        "scorer_id": IDENTIFIER,
        "scorer_sha256": HASH,
        "answer_policy_sha256": HASH,
        "metrics": array(METRIC_OBSERVATION),
        "limitations": STRING_LIST,
    }
)
