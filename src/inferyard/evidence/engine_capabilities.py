"""One read-bound interpretation of the historical Prism definition evidence."""

from dataclasses import dataclass

from inferyard.adapters.prism import BUILD
from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_evidence import TrialEvidence


@dataclass(frozen=True, slots=True)
class EngineCapabilities:
    prism_definition: bool
    sealed: bool

    @property
    def verified(self):
        return self.prism_definition and self.sealed


def bound_capabilities(root, manifest, *, reads=None):
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
    return EngineCapabilities(
        type(props) is dict and props.get("build_info") == BUILD,
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
