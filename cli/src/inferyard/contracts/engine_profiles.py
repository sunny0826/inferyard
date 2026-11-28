"""Explicit formal profiles; diagnostic support does not imply execution support."""

LEGACY_DIAGNOSTIC_ENGINES = ("vllm",)
DIAGNOSTIC_ENGINES = (
    *LEGACY_DIAGNOSTIC_ENGINES,
    "llama-cpp",
    "mlx-lm",
    "lmstudio",
    "ollama",
    "polystrata",
)
LOCAL_ADAPTER = "local_openai_v1"
LOCAL_CONFIG = "local-engine-config.v1"
LOCAL_OBSERVATION = "local-engine-observation.v1"
LOCAL_FAMILIES = ("llama-cpp", "mlx-lm", "vllm", "lmstudio", "polystrata")
DIRECTORY_FAMILIES = ("mlx-lm", "vllm")
POLYSTRATA_SUPPORTED_GGUF_ARCHS = (
    "qwen35moe",
    "gemma4",
    "lfm2moe",
    "flint",
    "glm5-next",
    "glm5next",
    "deepseek4",
    "kolibri1",
    "clover_moe",
)
REASONING_EFFORT_LEVELS = ("none", "low", "medium", "high")
TUNING_KEYS = (
    "flash_attention",
    "batch_size",
    "ubatch_size",
    "kv_cache_type_k",
    "kv_cache_type_v",
    "gpu_layers",
)
PROMPT_CACHE = ("enabled", "disabled")


def is_local(config):
    return config.get("definition") == LOCAL_CONFIG


def observes_before_idle(adapter):
    return adapter in ("kvmem", "ninfer", LOCAL_ADAPTER)
