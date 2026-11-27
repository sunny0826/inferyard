"""One read-bound interpretation of the historical Prism definition evidence."""

from dataclasses import dataclass

from inferyard.errors import EvidenceError
from inferyard.evidence.trial_evidence import TrialEvidence


@dataclass(frozen=True, slots=True)
class EngineCapabilities:
    prism_definition: bool
    sealed: bool

    @property
    def verified(self):
        return self.prism_definition and self.sealed


def bound_capabilities(root, manifest, *, reads=None):
    from inferyard.adapters.prism import BUILD
    from inferyard.contracts.template_identity import matches_prism_build

    own_reads = reads is None
    reads = reads if reads is not None else TrialEvidence(root)
    try:
        props = reads.observation("service_properties") or {}
    except EvidenceError as exc:
        if str(exc) == "original_evidence_unsealed" or (
            own_reads and str(exc) == "original_evidence_hash_mismatch"
        ):
            return EngineCapabilities(False, False)
        raise
    build_info = props.get("build_info") if type(props) is dict else None
    prism_ok = build_info == BUILD
    if not prism_ok and isinstance(build_info, str):
        try:
            cfg = reads.input("config")
        except EvidenceError:
            cfg = None
        if isinstance(cfg, dict):
            eng = cfg.get("engine")
            if isinstance(eng, dict) and eng.get("adapter") == "prism_llama_server_v1":
                prism_ok = matches_prism_build(eng.get("release"), build_info)
    return EngineCapabilities(
        prism_ok,
        observation_sealed(reads, "service_properties", manifest),
    )


def observation_sealed(reads, kind, manifest, *, stage=None, key=None):
    """A real logical member must exist in a sealed physical original."""
    if manifest is None:
        return False
    try:
        reference = reads.observation_reference(kind, stage=stage, key=key)
    except EvidenceError as exc:
        if str(exc) == "original_evidence_unsealed":
            return False
        raise
    return reference is not None and reference["path"] in manifest
