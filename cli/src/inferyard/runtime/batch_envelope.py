"""Strict batch.json envelope admission shared by readers and verification."""

from __future__ import annotations

from inferyard.analysis.comparison_types import JSONRecord, JSONValue
from inferyard.errors import ContractError, EvidenceError
from inferyard.evidence.formats import require_core
from inferyard.implementation_identity import validate_identity

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
    """Reject malformed batch metadata before any run or identity is trusted."""
    require_core(metadata, "batch")
    if (
        type(metadata) is not dict
        or type(metadata.get("diagnostic")) is not bool
        or not _hex_digest(metadata.get("plan_sha256"))
        or not _hex_digest(metadata.get("tool_source_sha256"))
        or set(metadata) - _BATCH_KEYS
    ):
        raise EvidenceError("batch_plan_identity_mismatch")
    if expected_plan_sha256 is not None and metadata["plan_sha256"] != expected_plan_sha256:
        raise EvidenceError("batch_plan_identity_mismatch")
    if "implementation_identity" in metadata:
        try:
            validate_identity(metadata["implementation_identity"])
        except (ContractError, KeyError, TypeError, IndexError, ValueError) as exc:
            raise EvidenceError("batch_plan_identity_mismatch") from exc
