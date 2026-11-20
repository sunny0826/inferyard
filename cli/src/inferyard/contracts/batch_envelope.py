"""Strict batch.json envelope admission shared by readers and verification."""

from __future__ import annotations

from inferyard.contracts.comparison_types import JSONRecord, JSONValue
from inferyard.contracts.format_versions import require_current
from inferyard.errors import EvidenceError

_BATCH_KEYS = {
    "schema_version",
    "plan_sha256",
    "diagnostic",
    "tool_source_sha256",
    "implementation_identity",
}


def _hex_digest(value: JSONValue) -> bool:
    return (
        type(value) is str
        and len(value) == 64
        and all(character in "0123456789abcdef" for character in value)
    )


def validate_batch_envelope(
    metadata: JSONRecord, expected_plan_sha256: str | None = None
) -> None:
    """Reject malformed batch metadata before any run is trusted."""
    require_current(metadata, "batch")
    has_source = "tool_source_sha256" in metadata
    has_legacy_identity = "implementation_identity" in metadata
    if (
        type(metadata) is not dict
        or type(metadata.get("diagnostic")) is not bool
        or not _hex_digest(metadata.get("plan_sha256"))
        or not (has_source or has_legacy_identity)
        or (has_source and not _hex_digest(metadata.get("tool_source_sha256")))
        or (has_legacy_identity and type(metadata.get("implementation_identity")) is not dict)
        or set(metadata) - _BATCH_KEYS
    ):
        raise EvidenceError("batch_plan_identity_mismatch")
    if expected_plan_sha256 is not None and metadata["plan_sha256"] != expected_plan_sha256:
        raise EvidenceError("batch_plan_identity_mismatch")
