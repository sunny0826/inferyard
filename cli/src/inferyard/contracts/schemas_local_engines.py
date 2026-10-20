"""New local-engine branches leave historical single-file configuration intact."""

from copy import deepcopy

from inferyard.contracts.engine_profiles import LOCAL_ADAPTER, LOCAL_CONFIG, LOCAL_FAMILIES
from inferyard.contracts.schemas_common import HASH, LABEL, TEXT, VERSION, array, enum, obj
from inferyard.contracts.schemas_config import CONFIG, CONFIG_DEFAULTS

ENVIRONMENT = obj(
    {
        "SGLANG_USE_MLX": enum("1"),
        "PYTHONDONTWRITEBYTECODE": enum("1"),
        "HF_HUB_OFFLINE": enum("1"),
        "TRANSFORMERS_OFFLINE": enum("1"),
        "SGLANG_RUN_ID": {"type": "string", "pattern": "[A-Za-z0-9][A-Za-z0-9_.-]{0,127}"},
        "PROMETHEUS_MULTIPROC_DIR": LABEL,
    },
    [],
)
WORKER_ENVIRONMENT = deepcopy(ENVIRONMENT)
WORKER_ENVIRONMENT["properties"].update(
    {
        "VLLM_WORKER_MULTIPROC_METHOD": enum("spawn"),
        "MLX_MAX_OPS_PER_BUFFER": enum("2000"),
        "TRANSFORMERS_NO_ADVISORY_WARNINGS": enum("1"),
        "VLLM_USE_V2_MODEL_RUNNER": enum("0"),
        "VLLM_OMP_NUM_THREADS_SET_BY_VLLM": enum("1"),
        "OMP_NUM_THREADS": {"type": "string", "pattern": "[1-9][0-9]{0,2}"},
        "SGLANG_USE_CUDA_IPC_TRANSPORT": enum("0"),
        "SGLANG_ENABLE_TORCH_COMPILE": enum("0"),
        "SGLANG_DISABLE_OUTLINES_DISK_CACHE": enum("0"),
        "SGLANG_ENABLE_DETERMINISTIC_INFERENCE": enum("0"),
    }
)

INSTALLATION = obj(
    {
        "schema_version": VERSION,
        "definition": enum("local-engine-installation.v1"),
        "family": enum(*LOCAL_FAMILIES),
        "executable": LABEL,
        "working_directory": LABEL,
        "runtime_roots": array(LABEL, 1),
        "environment": ENVIRONMENT,
    }
)
INSTALLATION["properties"]["models_root"] = LABEL
INSTALLATION["properties"]["worker_environment"] = WORKER_ENVIRONMENT

LOCAL_CONFIG_SCHEMA = deepcopy(CONFIG)
LOCAL_CONFIG_SCHEMA["properties"]["definition"] = enum(LOCAL_CONFIG)
LOCAL_CONFIG_SCHEMA["required"].append("definition")
LOCAL_CONFIG_SCHEMA["properties"]["model"] = obj(
    {
        "display_name": LABEL,
        "repo": LABEL,
        "revision": LABEL,
        "kind": enum("gguf", "directory"),
        "local_path": LABEL,
        "packing": LABEL,
        "assets_path": LABEL,
        "assets_sha256": HASH,
        "content_sha256": HASH,
        "template_path": LABEL,
        "template_sha256": HASH,
    }
)
LOCAL_CONFIG_SCHEMA["properties"]["engine"] = obj(
    {
        "adapter": enum(LOCAL_ADAPTER),
        "family": enum(*LOCAL_FAMILIES),
        "backend": enum("metal"),
        "installation_path": LABEL,
        "installation_sha256": HASH,
        "startup_args": array(TEXT),
        "working_directory": LABEL,
        "environment": ENVIRONMENT,
        "served_model": LABEL,
    }
)
LOCAL_CONFIG_SCHEMA["properties"]["generation"] = obj(
    {
        "temperature": {"type": "number", "const": 0},
        "max_tokens": {"type": "integer", "minimum": 1, "maximum": 8192},
    }
)
endpoint = LOCAL_CONFIG_SCHEMA["properties"]["endpoint"]["properties"]
endpoint.update(binding_path=LABEL, binding_sha256=HASH)
conditions = LOCAL_CONFIG_SCHEMA["properties"]["conditions"]
for field in ("threads", "threads_batch", "slots"):
    conditions["properties"].pop(field)
    conditions["required"].remove(field)
conditions["properties"]["cache_policy"] = enum("unknown")
LOCAL_CONFIG_SCHEMA["properties"]["safety"] = obj(
    {
        "interval_seconds": {"type": "number", "minimum": 0.1, "maximum": 5},
        "max_temperature_celsius": {"type": "number", "minimum": 1, "maximum": 85},
        "require_temperature": {"type": "boolean", "const": True},
        "check_environment": {"type": "boolean", "const": True},
    }
)
LOCAL_CONFIG_SCHEMA["required"].append("safety")
LOCAL_CONFIG_SCHEMA["properties"]["execution"]["properties"]["idle_wait_seconds"] = {
    "type": "number",
    "minimum": 1,
    "maximum": 120,
}
LOCAL_CONFIG_SCHEMA["properties"]["conditions"]["properties"]["context_size"] = {
    "type": "integer",
    "minimum": 1,
    "maximum": 32768,
}

LOCAL_CONFIG_INPUT = deepcopy(LOCAL_CONFIG_SCHEMA)
for section, defaults in CONFIG_DEFAULTS.items():
    specification = LOCAL_CONFIG_INPUT["properties"][section]
    for key, value in defaults.items():
        specification["required"].remove(key)
        specification["properties"][key]["default"] = deepcopy(value)
    if not specification["required"]:
        LOCAL_CONFIG_INPUT["required"].remove(section)
LOCAL_CONFIG_INPUT["properties"]["execution"]["properties"]["idle_wait_seconds"]["default"] = 30
