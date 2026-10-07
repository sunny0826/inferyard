"""One sealed manifest and one summary shape across execution modes."""

from inferyard.contracts.schemas_common import (
    BOOL,
    EXECUTION,
    HASH,
    IDENTIFIER,
    LABEL,
    NAT,
    POS,
    STRING_LIST,
    VERSION,
    array,
    enum,
    nullable,
    obj,
)
from inferyard.contracts.schemas_experiment import METRIC_OBSERVATION
from inferyard.contracts.schemas_observation import OBSERVATION
from inferyard.contracts.schemas_summary_cohorts import INPUT_LENGTHS, POSITIONS, TARGET_CHECK
from inferyard.contracts.schemas_summary_context import CACHE, MEASUREMENT_CONTEXT
from inferyard.contracts.schemas_summary_metrics import (
    DURATION,
    DURATION_QUALITY,
    IDLE_RSS,
    PERFORMANCE,
    QUALITY,
    RATE,
    RESOURCES,
)

MANIFEST = obj(
    {
        "schema_version": VERSION,
        "run_id": IDENTIFIER,
        "sealed": {"type": "boolean", "const": True},
        "files": {
            "type": "object",
            "additionalProperties": obj(
                {
                    "sha256": HASH,
                    "bytes": NAT,
                    "derived": BOOL,
                }
            ),
        },
        "kind": enum("check", "run"),
        "execution_mode": enum("single", "experiment"),
        "origin": enum("measured", "migrated"),
        "experiment_id": IDENTIFIER,
        "trial_id": IDENTIFIER,
    },
    ["schema_version", "run_id", "sealed", "files"],
)
COUNTS = obj(
    {
        "planned": nullable(NAT),
        "executed": NAT,
        "valid_executed": NAT,
        **{key: NAT for key in EXECUTION["enum"]},
        "budget_exhausted": NAT,
        "budget_exhausted_completed": NAT,
        "budget_exhausted_other_diagnostic": NAT,
        "request_limit": POS,
    },
    [
        "planned",
        "executed",
        "valid_executed",
        *EXECUTION["enum"],
        "budget_exhausted",
        "budget_exhausted_completed",
        "budget_exhausted_other_diagnostic",
    ],
)
SUMMARY = obj(
    {
        "schema_version": VERSION,
        "run_id": IDENTIFIER,
        "trial_id": IDENTIFIER,
        "experiment_id": IDENTIFIER,
        "protocol_kind": enum("fixed", "duration"),
        "completeness": enum("complete", "incomplete"),
        "scope_complete": BOOL,
        "evidence_complete": BOOL,
        "stop_reason": LABEL,
        "execution_parameters": obj(
            {
                "request_timeout_seconds": {"type": "number", "exclusiveMinimum": 0},
                "output_budget_tokens": POS,
            }
        ),
        "counts": COUNTS,
        "completion_rate": RATE,
        "quality": {"oneOf": [QUALITY, DURATION_QUALITY]},
        "performance": PERFORMANCE,
        "limitations": STRING_LIST,
        "metric_observations": array(METRIC_OBSERVATION),
        "resources": RESOURCES,
        "input_target_check": TARGET_CHECK,
        "input_lengths": INPUT_LENGTHS,
        "measurement_context": MEASUREMENT_CONTEXT,
        "duration": DURATION,
        "idle_rss": IDLE_RSS,
        "positions": POSITIONS,
        "cache_observations": CACHE,
    },
    [
        "schema_version",
        "run_id",
        "trial_id",
        "experiment_id",
        "protocol_kind",
        "completeness",
        "scope_complete",
        "evidence_complete",
        "stop_reason",
        "execution_parameters",
        "counts",
        "completion_rate",
        "quality",
        "performance",
        "limitations",
        "metric_observations",
        "resources",
        "input_target_check",
        "input_lengths",
        "measurement_context",
    ],
)

# Compatible optional disclosure; old sealed records have no observation envelope.
MANIFEST["properties"]["engine_observation"] = OBSERVATION
SUMMARY["properties"]["engine_observation"] = OBSERVATION
