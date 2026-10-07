"""Frozen configuration and user input defaults."""

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
    TEXT,
    VERSION,
    array,
    enum,
    obj,
)

CONFIG = obj(
    {
        "schema_version": VERSION,
        "device": obj({"id": IDENTIFIER, "client_position": enum("same_host_native")}),
        "model": obj(
            {
                "display_name": LABEL,
                "repo": LABEL,
                "revision": LABEL,
                "local_path": LABEL,
                "sha256": HASH,
                "packing": LABEL,
                "template_path": LABEL,
                "template_sha256": HASH,
            }
        ),
        "engine": obj(
            {
                "adapter": enum("prism_llama_server_v1", "llama_cpp_b11146_v1", "kvmem", "ninfer"),
                "release": LABEL,
                "binary_path": LABEL,
                "binary_sha256": HASH,
                "runtime_library_manifest": LABEL,
                "startup_args": array(TEXT),
                "backend": enum("cpu", "cuda", "metal"),
            }
        ),
        "endpoint": obj(
            {
                "url": LABEL,
                "server_pid": POS,
                "process_start_ticks": POS,
                "api_key_env": {"type": "string", "pattern": r"^[A-Za-z_][A-Za-z0-9_]*$"},
            },
            ["url", "server_pid", "process_start_ticks"],
        ),
        "bundle": obj({"path": LABEL, "version": LABEL}),
        "generation": obj(
            {
                "temperature": {"type": "number", "minimum": 0},
                "top_p": {"type": "number", "exclusiveMinimum": 0, "maximum": 1},
                "top_k": NAT,
                "min_p": {"type": "number", "minimum": 0, "maximum": 1},
                "presence_penalty": NUMBER,
                "repeat_penalty": {"type": "number", "exclusiveMinimum": 0},
                "seed": {"type": "integer"},
                "seed_support": enum("supported", "unsupported", "unknown"),
                "max_tokens": POS,
                "reasoning_mode": enum("off", "on"),
                "stop": array(LABEL),
            }
        ),
        "execution": obj(
            {
                "concurrency": {"type": "integer", "const": 1},
                "repeats": {"type": "integer", "const": 1},
                "order": enum("fixed"),
                "timeout_seconds": {"type": "number", "exclusiveMinimum": 0},
                "warmup_count": {"type": "integer", "minimum": 0, "maximum": 3},
                "warmup_prompt": LABEL,
                "probe_prompt": LABEL,
                "idle_wait_seconds": {"type": "number", "const": 5},
            }
        ),
        "telemetry": obj(
            {
                "interval_ms": POS,
                "baseline_seconds": {"type": "number", "exclusiveMinimum": 0},
                "system_memory_required": {"type": "boolean", "const": True},
                "service_rss_required": BOOL,
                "window_definition": enum("v1"),
            }
        ),
        "conditions": obj(
            {
                "context_size": POS,
                "threads": POS,
                "threads_batch": POS,
                "slots": {"type": "integer", "const": 1},
                "cache_policy": enum("disabled", "enabled", "unknown"),
                "ac_online": BOOL,
                "profile": LABEL,
                "governor": LABEL,
                "epp": LABEL,
                "model_loaded": BOOL,
                "background_load": LABEL,
            }
        ),
        "evidence": obj({"parameter_evidence": STRING_LIST, "service_evidence": STRING_LIST}),
        "output": obj(
            {
                "root": LABEL,
                "min_disk_bytes": {"type": "integer", "minimum": 5 * 1024**3},
                "min_available_memory_bytes": {"type": "integer", "minimum": 1024**3},
            }
        ),
    }
)

# Missing retains the historical strict EPP requirement. This opt-out is explicit
# in the frozen config and therefore participates in plan/source identity.
CONFIG["properties"]["execution"]["properties"]["require_fresh_process"] = BOOL

ENVIRONMENT_ADMISSION = obj(
    {
        "definition": enum("environment-admission.v2"),
        "required_fields": {
            "type": "array",
            "uniqueItems": True,
            "items": enum("ac_online", "profile", "governor", "epp", "macos_power_policy"),
        },
    }
)
CONFIG["properties"]["conditions"]["properties"]["environment_admission"] = ENVIRONMENT_ADMISSION
CONFIG["properties"]["conditions"]["properties"]["require_epp_match"] = BOOL
CONFIG["properties"]["conditions"]["properties"]["allow_unknown_environment"] = BOOL
CONFIG["properties"]["conditions"]["properties"]["macos_power_policy"] = obj(
    {
        "source": enum("macos.pmset.power-policy.v1"),
        "ac_online": BOOL,
        "low_power_mode": {"type": "integer", "enum": [0, 1]},
        "power_mode": {"type": ["integer", "string"], "enum": [0, 1, 2, "unsupported"]},
        "power_mode_supported": BOOL,
    }
)
CONFIG["properties"]["engine"]["properties"]["slots_debug"] = BOOL
CONFIG["properties"]["quantization_artifact_binding"] = obj(
    {
        "root": {
            "type": "string",
            "pattern": r"^(?:/|[A-Za-z]:[\\/]|\\\\[^\\/]+[\\/][^\\/]+[\\/]).*$",
        },
        "files": {"type": "object", "minProperties": 1, "additionalProperties": LABEL},
    }
)

# Optional for legacy v3 records; required for lab adapters by semantic validation.
CONFIG["properties"]["model"]["properties"].update(
    {
        "kind": enum("gguf", "ninfer"),
        "bytes": POS,
        "component_ledger_path": LABEL,
        "component_ledger_sha256": HASH,
    }
)
CONFIG["properties"]["engine"]["properties"].update(
    {
        "working_directory": LABEL,
        "observation_mode": enum("auto", "lab_required"),
        "environment": obj({"CUDA_VISIBLE_DEVICES": LABEL, "CUDA_CACHE_DISABLE": LABEL}, []),
        "asset_manifest": array(
            obj(
                {
                    "path": LABEL,
                    "role": enum("model", "engine", "library", "component_ledger", "template"),
                    "bytes": POS,
                    "sha256": HASH,
                }
            ),
            1,
        ),
    }
)

CONFIG_DEFAULTS = {
    "execution": {
        "concurrency": 1,
        "repeats": 1,
        "order": "fixed",
        "timeout_seconds": 180,
        "warmup_count": 3,
        "warmup_prompt": "请只回答：准备就绪",
        "probe_prompt": "请只用中文回答：中国的首都是哪里？只回答城市名。",
        "idle_wait_seconds": 5,
    },
    "telemetry": {
        "interval_ms": 500,
        "baseline_seconds": 5,
        "system_memory_required": True,
        "service_rss_required": False,
        "window_definition": "v1",
    },
    "evidence": {"parameter_evidence": [], "service_evidence": []},
    "output": {"min_disk_bytes": 5 * 1024**3, "min_available_memory_bytes": 8 * 1024**3},
}
CONFIG_INPUT = deepcopy(CONFIG)
for _section, _defaults in CONFIG_DEFAULTS.items():
    _spec = CONFIG_INPUT["properties"][_section]
    for _key, _value in _defaults.items():
        _spec["required"].remove(_key)
        _spec["properties"][_key]["default"] = deepcopy(_value)
    if not _spec["required"]:
        CONFIG_INPUT["required"].remove(_section)
