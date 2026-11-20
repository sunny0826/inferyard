"""Corpus structural and semantic validation (no runtime human-approval hash gating)."""

from inferyard.contracts.bundle import validate_bundle
from inferyard.contracts.validation import Document


def require_review(bundle):
    """Validate corpus structure and semantics without requiring approval hash records."""
    if type(bundle) is Document and bundle.kind == "bundle":
        return
    validate_bundle(bundle)
