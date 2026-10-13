"""Optional v3 disclosure separates endpoint capabilities from drain observations."""

from inferyard.contracts.schemas_common import BOOL, LABEL, NAT, TEXT, enum, nullable, obj

ENDPOINT = obj(
    {
        "available": {"type": ["boolean", "null"], "enum": [True, None]},
        "missing_reason": nullable(LABEL),
        "status_code": nullable(NAT),
        "body": nullable(TEXT),
    }
)
PATHS = (
    "/lab/v1/identity",
    "/lab/v1/lifecycle",
    "/health",
    "/v1/models",
    "/slots",
    "/metrics",
    "/props",
)
OBSERVATION = obj(
    {
        "mode": enum("lab", "native"),
        "engine": enum("kvmem", "ninfer"),
        "generation": enum("/v1/chat/completions"),
        "endpoints": obj({path: ENDPOINT for path in PATHS}),
        "request_id_scope": enum("engine", "client_only"),
        "engine_internal_drain": obj(
            {
                "value": nullable(BOOL),
                "scope": enum("capability_only"),
                "missing_reason": nullable(LABEL),
            }
        ),
        "native_cancel": obj({"value": {"type": "null"}, "missing_reason": LABEL}),
        "process_evidence": LABEL,
    }
)
