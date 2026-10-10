"""One read-bound interpretation of the historical Prism definition evidence."""

from dataclasses import dataclass

from inferyard.adapters.prism import BUILD
from inferyard.evidence.trial_evidence import TrialEvidence


@dataclass(frozen=True, slots=True)
class EngineCapabilities:
    prism_definition: bool
    sealed: bool

    @property
    def verified(self):
        return self.prism_definition and self.sealed


def bound_capabilities(root, manifest, *, reads=None):
    reads = reads if reads is not None else TrialEvidence(root)
    props = reads.observation("service_properties") or {}
    return EngineCapabilities(
        type(props) is dict and props.get("build_info") == BUILD,
        observation_sealed(reads, "service_properties", manifest),
    )


def observation_sealed(reads, kind, manifest, *, stage=None, key=None):
    """A real logical member must exist in a sealed physical original."""
    if manifest is None or not reads.has_observation(kind, stage=stage, key=key):
        return False
    return reads.observation_reference(kind, stage=stage, key=key)["path"] in manifest
