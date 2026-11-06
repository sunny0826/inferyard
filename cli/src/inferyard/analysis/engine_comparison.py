"""Gates for the same-GGUF engine comparison mode (ADR 056).

A single left/right pair checks identity only and stays blocked; the block
comparison in ``engine_blocks`` owns the performance verdict.
"""

from inferyard.contracts.engine_profiles import is_local

SUPPORTED_PAIRS = (frozenset(("llama-cpp", "lmstudio")),)
# Fields replaced by the local profile's installation and asset digests.
LEGACY_ENGINE_FIELDS = ("config.engine.release", "config.engine.binary_sha256")
# Fields that legitimately differ between engines in this mode.
ENGINE_DIFFERENCES = (
    "config.engine.family",
    "config.engine.installation_sha256",
    "config.engine.startup_args",
    "identity.runtime_library_hashes",
)
# A single pair never carries a verdict; compare-engines evaluates the
# per-request and block gates across interleaved runs.
PENDING = ("engine_verdict_requires_block_comparison",)


def engine_blockers(left, right):
    sides = (left, right)
    if not all(is_local(d["config"]) for d in sides):
        return ["engine_mode_requires_local_profile"]
    blockers = []
    families = frozenset(d["config"]["engine"].get("family") for d in sides)
    if len(families) != 2:
        blockers.append("engine_family_did_not_change")
    elif families not in SUPPORTED_PAIRS:
        blockers.append("unsupported_engine_pair")
    if any(d["config"]["model"].get("kind") != "gguf" for d in sides):
        blockers.append("engine_mode_requires_single_gguf")
    return blockers + list(PENDING)
