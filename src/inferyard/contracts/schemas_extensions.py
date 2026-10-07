"""Pure extension wire definitions; importing schemas never loads a live driver."""

from copy import deepcopy

from inferyard.contracts.schemas_common import (
    BOOL,
    HASH,
    IDENTIFIER,
    LABEL,
    POS,
    VERSION,
    array,
    enum,
    obj,
)

CLOSED_CONCURRENCY = obj(
    {
        "schema_version": VERSION,
        "definition": enum("closed_concurrency.v1"),
        "case_ids": array(IDENTIFIER, 1),
        "concurrency": {"type": "integer", "enum": [1, 2, 4]},
        "server_slots": {"type": "integer", "enum": [1, 2, 4]},
        "timeout_seconds": {"type": "number", "exclusiveMinimum": 0},
        "drain_timeout_seconds": {"type": "number", "exclusiveMinimum": 0},
        "max_wall_seconds": {"type": "number", "exclusiveMinimum": 0},
        "latency_limit_ms": {"type": "number", "exclusiveMinimum": 0},
        "require_quality": BOOL,
        "max_dispatch_delay_ns": POS,
        "max_client_cpu_fraction": {"type": "number", "exclusiveMinimum": 0, "maximum": 1},
    }
)

NATIVE_ARGS = obj(
    {
        "a": {"type": "integer", "minimum": -1_000_000, "maximum": 1_000_000},
        "b": {"type": "integer", "minimum": -1_000_000, "maximum": 1_000_000},
    }
)

NATIVE_LOOKUP = obj({"key": enum("alpha", "beta")})

NATIVE_CASE = obj(
    {
        "case_id": IDENTIFIER,
        "prompt": LABEL,
        "expected_tool": enum("add", "lookup"),
        "expected_arguments": {"oneOf": [NATIVE_ARGS, NATIVE_LOOKUP]},
        "expected_final": LABEL,
    }
)

NATIVE_TOOLS = obj(
    {
        "schema_version": VERSION,
        "definition": enum("native_tools.v1"),
        "cases": array(NATIVE_CASE, 1),
        "timeout_seconds": {"type": "number", "exclusiveMinimum": 0},
        "max_calls": POS,
        "max_rounds": POS,
        "track": enum("capability_diagnostic", "reviewed_formal"),
    }
)

NATIVE_TOOLS["properties"]["content_review_sha256"] = {
    "type": "string",
    "pattern": "^[0-9a-f]{64}$",
}

TOTAL_OBSERVER_CONTROL = obj(
    {
        "schema_version": VERSION,
        "definition": enum("total_observer_control.v1"),
        "case_ids": array(IDENTIFIER, 1),
        "tolerance_ratio": {"type": "number", "minimum": 0, "exclusiveMaximum": 1},
        "max_wall_seconds": {"type": "number", "exclusiveMinimum": 0},
        "max_guard_gap_ns": POS,
        "tool_source_sha256": HASH,
        "config_sha256": HASH,
        "bundle_sha256": HASH,
        "guardian_policy_sha256": HASH,
    }
)

TOTAL_TRIAL_CONTROL = deepcopy(TOTAL_OBSERVER_CONTROL)
TOTAL_TRIAL_CONTROL["properties"].update(
    {
        "definition": enum("total_observer_control.v2"),
        "trial_plan_path": LABEL,
        "trial_plan_sha256": HASH,
        "trial_id": IDENTIFIER,
    }
)
TOTAL_TRIAL_CONTROL["required"].extend(["trial_plan_path", "trial_plan_sha256", "trial_id"])
