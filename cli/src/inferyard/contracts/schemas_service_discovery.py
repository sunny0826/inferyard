"""Service discovery CommandResult contract; independent of live collectors."""

from inferyard.contracts.schemas_common import BOOL, LABEL, POS, array, enum, nullable, obj

PROBE_REASONS = (
    "models_timeout",
    "models_unreachable",
    "models_http_1xx",
    "models_http_3xx",
    "models_http_4xx",
    "models_http_5xx",
    "models_invalid_response",
    "models_empty",
    "models_response_too_large",
    "health_timeout",
    "health_unreachable",
)
LIMITATIONS = (
    "listener_table_incomplete",
    "listener_ownership_incomplete",
    "process_identity_unavailable",
    "listener_enumeration_failed",
    "service_discovery_unsupported_platform",
    "service_discovery_error",
)
ENDPOINT = obj(
    {
        "url": LABEL,
        "health": nullable(enum("1xx", "2xx", "3xx", "4xx", "5xx")),
        "models": nullable(array(LABEL, minimum=1)),
        "reason": nullable(enum(*PROBE_REASONS)),
        "ambiguous_listener": BOOL,
    }
)
ENDPOINT["allOf"] = [
    {
        "if": {"properties": {"models": {"type": "null"}}},
        "then": {"properties": {"reason": enum(*PROBE_REASONS[:-2])}},
        "else": {"properties": {"reason": nullable(enum(*PROBE_REASONS[-2:]))}},
    }
]
SERVICE_DISCOVERY = obj(
    {
        "command": enum("service discover"),
        "status": enum("discovered", "partial", "error"),
        "completeness": enum("complete", "incomplete"),
        "run_id": {"type": "null"},
        "evidence_dir": {"type": "null"},
        "limitations": array(enum(*LIMITATIONS)),
        "details": obj(
            {
                "model_requests_sent": {"type": "integer", "const": 0},
                "services": array(
                    obj(
                        {
                            "pid": POS,
                            "process_name": LABEL,
                            "process_start_ticks": POS,
                            "endpoints": array(ENDPOINT, minimum=1),
                        }
                    )
                ),
            }
        ),
    }
)
