"""Frozen engine tuning for the engine comparison mode (ADR 056).

llama-server tuning is read from the frozen startup arguments; local binding
verifies that the bound process runs exactly those arguments. LM Studio load
parameters are not exposed by any bound observation, so declaring them is
rejected instead of recorded as unverifiable.
"""

from inferyard.contracts.engine_profiles import TUNING_KEYS
from inferyard.contracts.validation import ContractError

# Option spellings accepted by llama-server; values are kept as frozen text.
LLAMA_OPTIONS = {
    "flash_attention": ("-fa", "--flash-attn"),
    "batch_size": ("-b", "--batch-size"),
    "ubatch_size": ("-ub", "--ubatch-size"),
    "kv_cache_type_k": ("-ctk", "--cache-type-k"),
    "kv_cache_type_v": ("-ctv", "--cache-type-v"),
    "gpu_layers": ("-ngl", "--gpu-layers", "--n-gpu-layers"),
}
OBSERVABLE = {"llama-cpp": frozenset(TUNING_KEYS), "lmstudio": frozenset()}


def llama_arguments(args):
    """Return the last value given for each tuning option; absent options are omitted."""
    observed = {}
    for index, arg in enumerate(args):
        name, _, inline = arg.partition("=")
        for key, spellings in LLAMA_OPTIONS.items():
            if name not in spellings:
                continue
            if inline:
                observed[key] = inline
            elif index + 1 < len(args):
                observed[key] = args[index + 1]
            else:
                raise ContractError("config.engine.startup_args", key + " without value")
    return observed


def validate_tuning(engine):
    tuning = engine.get("tuning")
    if tuning is None:
        return
    family = engine["family"]
    declared = {key: value for key, value in tuning.items() if key != "prompt_cache"}
    if family not in OBSERVABLE or not set(declared) <= OBSERVABLE[family]:
        raise ContractError("config.engine.tuning", "tuning not observable for " + family)
    if "prompt_cache" in tuning and family != "llama-cpp":
        raise ContractError("config.engine.tuning.prompt_cache", "not controllable for " + family)
    if family == "llama-cpp":
        observed = llama_arguments(engine["startup_args"])
        for key, value in declared.items():
            if observed.get(key) != value:
                raise ContractError("config.engine.tuning." + key, "differs from startup_args")


def observe(config):
    """Each key maps to an observed value or None with a missing reason."""
    engine = config["engine"]
    family = engine.get("family")
    observed = llama_arguments(engine["startup_args"]) if family == "llama-cpp" else {}
    rows = {}
    for key in TUNING_KEYS:
        if family == "llama-cpp":
            value = observed.get(key, "engine_default")
            rows[key] = {"value": value, "source": "verified_startup_args", "missing_reason": None}
        else:
            rows[key] = {
                "value": None,
                "source": None,
                "missing_reason": family + "_load_parameters_not_exposed",
            }
    return rows


def prompt_cache_disabled(config):
    tuning = config["engine"].get("tuning") or {}
    return (
        config["engine"].get("family") == "llama-cpp" and tuning.get("prompt_cache") == "disabled"
    )
