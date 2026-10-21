"""Optional v3 disclosure separates endpoint capabilities from drain observations."""

from copy import deepcopy

from inferyard.contracts.engine_profiles import LOCAL_ADAPTER, LOCAL_FAMILIES, LOCAL_OBSERVATION
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

LOCAL_ENGINE_OBSERVATION = deepcopy(OBSERVATION)
LOCAL_ENGINE_OBSERVATION["properties"].update(
    definition=enum(LOCAL_OBSERVATION),
    family=enum(*LOCAL_FAMILIES),
    mode=enum("native"),
    engine=enum(LOCAL_ADAPTER),
    endpoints=obj(
        {path: ENDPOINT for path in (*PATHS, "/version", "/get_server_info", "lms:ps")},
        [],
    ),
)
LOCAL_ENGINE_OBSERVATION["required"].extend(["definition", "family"])
OBSERVATION = {"oneOf": [OBSERVATION, LOCAL_ENGINE_OBSERVATION]}
