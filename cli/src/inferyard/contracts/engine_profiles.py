"""Explicit formal profiles; diagnostic support does not imply execution support."""

LOCAL_ADAPTER = "local_openai_v1"
LOCAL_CONFIG = "local-engine-config.v1"
LOCAL_OBSERVATION = "local-engine-observation.v1"
LOCAL_FAMILIES = ("llama-cpp", "mlx-lm", "vllm", "sglang", "lmstudio")
DIRECTORY_FAMILIES = ("mlx-lm", "vllm", "sglang")


def is_local(config):
    return config.get("definition") == LOCAL_CONFIG


def observes_before_idle(adapter):
    return adapter in ("kvmem", "ninfer", LOCAL_ADAPTER)
