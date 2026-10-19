"""Portable context speed result, including strict unreadable-source branch."""

from inferyard.contracts.schemas_common import (
    BOOL,
    EXECUTION,
    HASH,
    IDENTIFIER,
    LABEL,
    POS,
    STRING_LIST,
    VERSION,
    array,
    enum,
    nullable,
    obj,
)
from inferyard.contracts.schemas_events import EVENT_PAYLOADS
from inferyard.contracts.schemas_evidence_reference import EVIDENCE_REFERENCE as REF

NONNEGATIVE = {"type": "number", "minimum": 0}
CONTEXT = {"type": "integer", "enum": [65536, 131072, 262144]}
FIVE = {"type": "integer", "const": 5}
COUNT = {"type": "integer", "minimum": 0, "maximum": 5}


def value(spec):
    return obj(
        {"value": nullable(spec), "missing_reason": nullable(LABEL), "evidence_refs": array(REF)}
    )


COLUMNS = obj(
    {
        "context": value(CONTEXT),
        "prompt_length": value(POS),
        **{k: value(NONNEGATIVE) for k in ("time", "prefill", "decode")},
    }
)
STATISTICS = obj({k: NONNEGATIVE for k in ("median", "min", "max")})
SAMPLE = obj(
    {
        "trial_id": IDENTIFIER,
        "repeat_index": {"type": "integer", "minimum": 0, "maximum": 4},
        "case_id": enum("context-speed-64k", "context-speed-128k", "context-speed-256k"),
        "run": nullable(
            obj(
                {
                    "path": LABEL,
                    "run_id": IDENTIFIER,
                    "inputs": REF,
                    "manifest_sha256": nullable(HASH),
                    "integrity": enum("sealed", "unsealed"),
                }
            )
        ),
        "request_id": nullable(IDENTIFIER),
        "execution_state": EXECUTION,
        "error_category": nullable(LABEL),
        "stop_reason": value(LABEL),
        "measurement": obj(
            {"valid": BOOL, "reasons": STRING_LIST, "cohort_sha256": nullable(HASH)}
        ),
        "columns": COLUMNS,
        "proof": obj(
            {
                "identity": array(REF),
                "token_budget": nullable(REF),
                "request": nullable(REF),
                "final_timing": nullable(
                    obj({"file": REF, "event_seq": POS, "record": EVENT_PAYLOADS["engine_timings"]})
                ),
                "fixed_output": array(REF),
            }
        ),
    }
)
SUMMARY = obj(
    {
        "trial_ids": {**array(IDENTIFIER, 5), "maxItems": 5},
        "columns": obj(
            {
                "context": value(CONTEXT),
                "prompt_length": value(POS),
                **{k: STATISTICS for k in ("time", "prefill", "decode")},
            }
        ),
    }
)
BASE = {"schema_version": VERSION, "kind": enum("context_speed_result.v1")}
READABLE = obj(
    {
        **BASE,
        "read_state": enum("readable"),
        "profile": enum("context-speed.v1"),
        "source": obj(
            {
                "batch_metadata": REF,
                "plan": REF,
                "config": REF,
                "bundle": REF,
                "plan_sha256": HASH,
                "experiment_id": IDENTIFIER,
                "workload_id": IDENTIFIER,
            }
        ),
        "metadata": obj(
            {
                "model": obj({"display_name": LABEL, "sha256": HASH, "template_sha256": HASH}),
                "engine": obj(
                    {
                        "adapter": enum("prism_llama_server_v1"),
                        "release": LABEL,
                        "binary_sha256": HASH,
                    }
                ),
                "bundle": obj(
                    {"bundle_id": enum("zh-context-speed"), "version": enum("2026.10.1")}
                ),
                "selected_context_tokens": CONTEXT,
            }
        ),
        "execution": obj(
            {
                "counts": obj({"planned": FIVE, **{k: COUNT for k in EXECUTION["enum"]}}),
                "stop_reason": value(LABEL),
            }
        ),
        "measurement": obj(
            {"status": enum("complete", "incomplete"), "valid_count": COUNT, "reasons": STRING_LIST}
        ),
        "samples": {**array(SAMPLE, 5), "maxItems": 5},
        "summary": nullable(SUMMARY),
    }
)
ERROR = obj(
    {
        **BASE,
        "read_state": enum("error"),
        "error": obj({"code": LABEL, "source_path": nullable(LABEL)}),
    }
)
CONTEXT_SPEED_RESULT = {"oneOf": [READABLE, ERROR]}
