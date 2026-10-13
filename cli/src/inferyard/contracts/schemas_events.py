"""Unified request, duration-window and collector event envelopes."""

from inferyard.contracts.schemas_common import (
    BOOL,
    HASH,
    IDENTIFIER,
    LABEL,
    NAT,
    PHASE,
    POS,
    TEXT,
    VERSION,
    array,
    enum,
    nullable,
    obj,
)
from inferyard.contracts.schemas_config import CONFIG
from inferyard.contracts.schemas_memory import MEMORY_SAMPLE
from inferyard.contracts.schemas_resources import RESOURCE_SAMPLE
from inferyard.contracts.schemas_sensors import SENSOR_SAMPLE
from inferyard.contracts.schemas_tasks import SCORE

EVENT_BASE = {
    "schema_version": VERSION,
    "experiment_id": IDENTIFIER,
    "trial_id": IDENTIFIER,
    "run_id": IDENTIFIER,
    "seq": POS,
    "phase": PHASE,
    "request_id": nullable(IDENTIFIER),
    "monotonic_ns": NAT,
    "clock_id": IDENTIFIER,
    "utc": LABEL,
}
MESSAGES = array(obj({"role": enum("system", "user", "assistant"), "content": TEXT}), 1)
REQUEST_BODY_FIELDS = {
    "model": LABEL,
    "stream": BOOL,
    "generation": CONFIG["properties"]["generation"],
}
REQUEST = obj(
    {
        "case_id": nullable(IDENTIFIER),
        "plan_index": nullable(NAT),
        "attempt": {"type": "integer", "const": 1},
        "body": {
            "oneOf": [
                obj({**REQUEST_BODY_FIELDS, "messages": MESSAGES}),
                obj({**REQUEST_BODY_FIELDS, "messages_sha256": HASH}),
            ]
        },
    }
)
TERMINAL = obj(
    {
        "execution_state": enum("completed", "failed", "cancelled", "invalid"),
        "raw_finish_reason": nullable(TEXT),
        "budget_exhausted": BOOL,
        "error_category": nullable(LABEL),
        "protocol_complete": BOOL,
        "t_send_ns": NAT,
        "t_first_content_ns": nullable(NAT),
        "t_first_answer_ns": nullable(NAT),
        "t_terminal_ns": NAT,
        "content": TEXT,
        "reasoning": TEXT,
        "completion_tokens": nullable(NAT),
        "token_source": nullable(LABEL),
        "token_scope": nullable(LABEL),
    }
)
BASE_PAYLOADS = {
    "request_started": REQUEST,
    "content": obj({"text": TEXT}),
    "reasoning": obj({"text": TEXT}),
    "usage": obj(
        {
            "completion_tokens": nullable(NAT),
            "prompt_tokens": nullable(NAT),
            "source": LABEL,
            "scope": nullable(LABEL),
        }
    ),
    "finish": obj({"raw_finish_reason": LABEL}),
    "protocol_end": obj({}),
    "failure": obj({"category": LABEL, "message": TEXT}),
    "request_finished": TERMINAL,
    "score": SCORE,
    "idle_observed": obj({"state": enum("idle", "busy", "unknown"), "source": LABEL}),
    "run_stopped": obj({"reason": LABEL}),
}
BASE_PAYLOADS["idle_observed"]["properties"]["lab_snapshot"] = {"type": "object"}

EVENT_PAYLOADS = {
    "native_wire": obj(
        {
            "client_request_id": {"type": "string", "pattern": r"^[a-f0-9]{32}$"},
            "text": TEXT,
        }
    ),
    "native_observed": obj(
        {
            "signals": {"type": "object"},
            "scope": enum("native_endpoints"),
            "engine_internal_drain": {"type": "null"},
            "missing_reason": LABEL,
            "cancellation": nullable(
                obj(
                    {
                        "client_action": enum("close_http_response_context"),
                        "native_cancel": {"type": "null"},
                        "missing_reason": LABEL,
                    }
                )
            ),
        }
    ),
    "lab_wire": obj(
        {
            "request_id": {"type": "string", "pattern": r"^[a-f0-9]{32}$"},
            "server_instance_id": {"type": "string", "pattern": r"^[a-f0-9]{32}$"},
            "text": TEXT,
        }
    ),
    "lab_usage": obj(
        {
            **{
                key: nullable(NAT)
                for key in (
                    "prompt_tokens",
                    "completion_tokens",
                    "cached_tokens",
                    "reasoning_tokens",
                )
            },
            "usage_missing_reasons": obj(
                {
                    key: nullable(LABEL)
                    for key in (
                        "prompt_tokens",
                        "completion_tokens",
                        "cached_tokens",
                        "reasoning_tokens",
                    )
                }
            ),
        }
    ),
    **BASE_PAYLOADS,
    "duration_started": obj(
        {
            "start_ns": NAT,
            "admission_deadline_ns": NAT,
            "drain_deadline_ns": NAT,
            "request_limit": {"type": "integer", "minimum": 1},
        }
    ),
    "duration_closed": obj(
        {
            "reason": enum(
                "duration_elapsed",
                "request_limit_reached",
                "drain_deadline_exceeded",
                "interrupted",
            ),
            "returned_requests": NAT,
            "completed_requests": NAT,
            "closed_ns": NAT,
        }
    ),
    "http_response": obj({"status_code": {"type": "integer", "minimum": 100, "maximum": 599}}),
    "arrival_capture": obj({"streaming": BOOL, "source": enum("decoded_delta")}),
    "block_arrived": obj(
        {"index": {"type": "integer", "minimum": 1}, "channel": enum("content", "reasoning")}
    ),
    "engine_timings": obj(
        {
            "definition": enum("prism.adfffbe41.timings.v1"),
            "final": BOOL,
            "speculative": BOOL,
            "missing_reason": nullable(enum("invalid_engine_timings")),
            **{k: nullable(NAT) for k in ("cache_n", "prompt_n", "predicted_n")},
            **{
                k: nullable({"type": "number", "minimum": 0}) for k in ("prompt_ms", "predicted_ms")
            },
        }
    ),
}
EVENT = {
    "oneOf": [
        obj(
            {
                **EVENT_BASE,
                "event_type": enum(kind),
                "data": SCORE if kind == "score" else data,
            }
        )
        for kind, data in EVENT_PAYLOADS.items()
    ]
}
SAMPLE = {"oneOf": [MEMORY_SAMPLE, RESOURCE_SAMPLE, SENSOR_SAMPLE]}

SELECTION = obj(
    {
        "schema_version": VERSION,
        "run_id": IDENTIFIER,
        "trial_id": IDENTIFIER,
        "case_ids": array(IDENTIFIER, 1),
        "scorer_sha256": HASH,
        "config_sha256": HASH,
        "bundle_sha256": HASH,
        "parent_events_sha256": nullable(HASH),
    }
)
