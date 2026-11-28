"""Standalone model serve plan contract; independent of live platform backends."""

from inferyard.contracts.engine_profiles import LOCAL_ADAPTER
from inferyard.contracts.schemas_common import BOOL, LABEL, POS, TEXT, array, enum, obj

SERVE_FAMILIES = ("llama-cpp", "mlx-lm", "vllm", "polystrata")
SERVE_ADAPTERS = (LOCAL_ADAPTER, "prism_llama_server_v1")
SERVE_MODEL_KINDS = ("gguf", "directory", "strata_pack")
_PORT_1_65535 = r"(?:[1-9][0-9]{0,3}|[1-5][0-9]{4}|6[0-4][0-9]{3}|65[0-4][0-9]{2}|655[0-2][0-9]|6553[0-5])"
LOOPBACK_ENDPOINT = {
    "type": "string",
    "pattern": rf"^http://(127\.0\.0\.1|localhost|\[::1\]):{_PORT_1_65535}$",
}

SERVE_PLAN = obj(
    {
        "argv": array(LABEL, 1),
        "environment": {"type": "object", "additionalProperties": TEXT},
        "working_directory": LABEL,
        "endpoint": LOOPBACK_ENDPOINT,
        "model_path": LABEL,
        "model_kind": enum(*SERVE_MODEL_KINDS),
        "context_size": POS,
        "family": enum(*SERVE_FAMILIES),
        "adapter": enum(*SERVE_ADAPTERS),
        "local": BOOL,
    }
)
