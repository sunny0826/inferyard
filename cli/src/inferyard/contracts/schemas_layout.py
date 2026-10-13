"""Run layout 2 envelopes; inner domain documents remain core schema 3."""

from inferyard.contracts.schemas_common import (
    HASH,
    IDENTIFIER,
    LABEL,
    NAT,
    POS,
    TEXT,
    VERSION,
    enum,
    nullable,
    obj,
)

LAYOUT_VERSION = {"type": "integer", "const": 2}
INPUT_KINDS = ("run", "plan", "selection", "config", "bundle")
OBSERVATION_STAGES = {
    **{
        kind: ("prepare",)
        for kind in (
            "parent",
            "service_binding",
            "service_reuse",
            "safety_policy",
            "token_budgets",
            "input_target_check",
            "collector",
            "observer",
            "boundary_observer",
        )
    },
    "environment": ("start", "end"),
    "service_properties": ("pre_idle", "post_idle"),
    "engine_capabilities": ("pre_idle",),
    "recovery": ("post_idle",),
    "effective_parameters": ("ordinary", "stream"),
    "identity": ("post_probe", "finalizing"),
    "native_clean_receipt": ("client_complete",),
    "safety_check": ("finalizing",),
    "execution_budget": ("finalizing",),
}
INPUTS = obj(
    {
        "schema_version": VERSION,
        "evidence_layout_version": LAYOUT_VERSION,
        "run_id": IDENTIFIER,
        "documents": obj(
            {kind: obj({"payload": {"type": "object"}, "sha256": HASH}) for kind in INPUT_KINDS}
        ),
        "configuration_source": nullable(
            obj(
                {
                    "media_type": enum("application/json", "application/toml"),
                    "encoding": enum("base64"),
                    "content": TEXT,
                    "sha256": HASH,
                    "bytes": NAT,
                }
            )
        ),
        "configuration_source_missing_reason": nullable(LABEL),
    }
)
OBSERVATION_RECORD = {
    "oneOf": [
        obj(
            {
                "schema_version": VERSION,
                "evidence_layout_version": LAYOUT_VERSION,
                "run_id": IDENTIFIER,
                "trial_id": IDENTIFIER,
                "seq": POS,
                "kind": enum(kind),
                "stage": enum(*stages),
                "key": IDENTIFIER if kind == "native_clean_receipt" else {"type": "null"},
                "clock_id": IDENTIFIER,
                "monotonic_ns": NAT,
                "utc": LABEL,
                "payload": {"type": "object"},
                "payload_sha256": HASH,
            }
        )
        for kind, stages in OBSERVATION_STAGES.items()
    ]
}
PREPARED_REQUEST = obj(
    {
        "schema_version": VERSION,
        "evidence_layout_version": LAYOUT_VERSION,
        "run_id": IDENTIFIER,
        "trial_id": IDENTIFIER,
        "request_id": IDENTIFIER,
        "ordinal": POS,
        "phase": enum("probe", "warmup", "formal"),
        "body": {"type": "object"},
        "body_sha256": HASH,
    }
)
