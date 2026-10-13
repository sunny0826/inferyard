"""Atomic inputs envelope construction from frozen documents and real source bytes."""

import base64
import hashlib

from inferyard import SCHEMA_VERSION
from inferyard.contracts.validation import ContractError, validate_document
from inferyard.evidence.storage import EvidenceError, json_bytes


def frozen_inputs(run_id, documents, configuration_source, redactor):
    source = (
        configuration_source
        if configuration_source is not None
        else {
            "media_type": None,
            "data": None,
            "missing_reason": "not_persisted",
        }
    )
    try:
        if type(source) is not dict or set(source) != {"media_type", "data", "missing_reason"}:
            raise ValueError
        raw = source["data"]
        encoded = None
        if raw is not None:
            if type(raw) is not bytes or source["missing_reason"] is not None:
                raise ValueError
            raw = redactor.text(raw.decode("utf-8")).encode("utf-8")
            encoded = {
                "media_type": source["media_type"],
                "encoding": "base64",
                "content": base64.b64encode(raw).decode("ascii"),
                "sha256": hashlib.sha256(raw).hexdigest(),
                "bytes": len(raw),
            }
        elif source["media_type"] is not None:
            raise ValueError
        envelope = {
            "schema_version": SCHEMA_VERSION,
            "evidence_layout_version": 2,
            "run_id": run_id,
            "documents": {
                name: {
                    "payload": payload,
                    "sha256": hashlib.sha256(json_bytes(payload)).hexdigest(),
                }
                for name, payload in documents.items()
            },
            "configuration_source": encoded,
            "configuration_source_missing_reason": source["missing_reason"],
        }
        validate_document("inputs", envelope)
    except (ValueError, UnicodeError, ContractError) as exc:
        raise EvidenceError("invalid_inputs_container") from exc
    return envelope
