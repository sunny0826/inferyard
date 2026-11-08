"""Gates for the engine comparison mode (ADR 056).

A single left/right pair checks identity only and stays blocked; the block
comparison in ``engine_blocks`` owns the performance verdict.
"""

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.contracts.engine_profiles import is_local

SAME_ASSET = frozenset(("llama-cpp", "lmstudio"))
# Different model formats of one upstream model: the verdict covers the
# engine together with its model format and cannot be attributed to the engine.
CROSS_FORMAT = frozenset(("llama-cpp", "mlx-lm"))
SUPPORTED_PAIRS = (SAME_ASSET, CROSS_FORMAT)
# Fields the local profile never fills, each checked through its replacement:
# installation_sha256 (release, binary, runtime closure), content_sha256 or
# assets_sha256 (model bytes), and the per-request cold-cache gate in
# engine_blocks (the local schema freezes cache_policy as "unknown").
LEGACY_ENGINE_FIELDS = (
    "config.engine.release",
    "config.engine.binary_sha256",
    "identity.runtime_library_hashes",
    "config.model.sha256",
    "config.conditions.cache_policy",
)
# Fields that legitimately differ between engines in this mode.
ENGINE_DIFFERENCES = (
    "config.engine.family",
    "config.engine.installation_sha256",
    "config.engine.startup_args",
    "identity.runtime_library_hashes",
)
MODEL_FORMAT_DIFFERENCES = (
    "config.model.kind",
    "config.model.content_sha256",
    "config.model.assets_sha256",
    "config.model.repo",
    "config.model.revision",
    "config.model.sha256",
    "config.model.packing",
    "config.model.template_sha256",
)
# A single pair never carries a verdict; compare-engines evaluates the
# per-request and block gates across interleaved runs.
PENDING = ("engine_verdict_requires_block_comparison",)


def families(left, right):
    return frozenset(d["config"]["engine"].get("family") for d in (left, right))


def allowed_differences(left, right):
    if families(left, right) == CROSS_FORMAT:
        return ENGINE_DIFFERENCES + MODEL_FORMAT_DIFFERENCES
    return ENGINE_DIFFERENCES


def config_pair_blockers(
    left_config: JSONRecord, right_config: JSONRecord
) -> list[str]:
    """Supported engine pairs for plan freezing and frozen-plan reading (ADR 056)."""
    left = {"config": left_config}
    right = {"config": right_config}
    blockers = [item for item in engine_blockers(left, right) if item not in PENDING]
    if families(left, right) == SAME_ASSET:
        for key in ("content_sha256", "assets_sha256"):
            if left_config["model"].get(key) != right_config["model"].get(key):
                blockers.append(f"config.model.{key}:different")
    return blockers


def engine_blockers(left, right):
    sides = (left, right)
    if not all(is_local(d["config"]) for d in sides):
        return ["engine_mode_requires_local_profile"]
    blockers = []
    pair = families(left, right)
    if len(pair) != 2:
        blockers.append("engine_family_did_not_change")
    elif pair not in SUPPORTED_PAIRS:
        blockers.append("unsupported_engine_pair")
    if pair == SAME_ASSET and any(d["config"]["model"].get("kind") != "gguf" for d in sides):
        blockers.append("engine_mode_requires_single_gguf")
    return blockers + list(PENDING)
