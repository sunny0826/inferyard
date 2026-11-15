"""Identify retired engine-comparison.v1 artifacts; never recompute or render them."""

from inferyard.errors import EvidenceError, UnsupportedFormat

DEFINITION = "engine-comparison.v1"
PRODUCER = {"tool_source_sha256", "template_sha256"}


def _hash(value):
    return type(value) is str and len(value) == 64 and all(c in "0123456789abcdef" for c in value)


def reject_legacy(saved):
    """Reject a structurally recognizable v1 artifact after limited identification."""
    if (
        type(saved) is not dict
        or saved.get("definition") != DEFINITION
        or type(saved.get("source_runs")) is not list
        or not saved["source_runs"]
        or any(type(ref) is not dict for ref in saved["source_runs"])
        or type(saved.get("producer")) is not dict
        or set(saved["producer"]) != PRODUCER
        or not all(_hash(value) for value in saved["producer"].values())
    ):
        raise EvidenceError("engine_comparison_format_invalid")
    raise UnsupportedFormat("engine-comparison", 1, (2,))
