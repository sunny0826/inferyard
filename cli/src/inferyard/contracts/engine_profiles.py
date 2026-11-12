"""Explicit formal profiles; diagnostic support does not imply execution support."""

# Diagnostics may name these engines; execution support stays profile-declared.
LEGACY_DIAGNOSTIC_ENGINES = ("vllm", "sglang")
DIAGNOSTIC_ENGINES = (*LEGACY_DIAGNOSTIC_ENGINES, "llama-cpp", "mlx-lm", "lmstudio", "ollama")
LOCAL_ADAPTER = "local_openai_v1"
LOCAL_CONFIG = "local-engine-config.v1"
LOCAL_OBSERVATION = "local-engine-observation.v1"
LOCAL_FAMILIES = ("llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio")
DIRECTORY_FAMILIES = ("mlx-lm", "vllm", "sglang")
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
