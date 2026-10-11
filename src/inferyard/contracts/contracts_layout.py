"""Content and cross-field invariants for the run container boundary."""

import base64
import binascii
import hashlib

from inferyard.contracts.validation import ContractError, _validate, validate_document


def content_digest(value):
    from inferyard.evidence.storage import json_bytes

    return hashlib.sha256(json_bytes(value)).hexdigest()


def validate_layout(kind, data):
    if kind == "inputs":
        for name, member in data["documents"].items():
            payload = member["payload"]
            validate_document(name, payload)
            if member["sha256"] != content_digest(payload):
                raise ContractError("inputs.documents", "member content hash mismatch")
        run, selection = (data["documents"][key]["payload"] for key in ("run", "selection"))
        if data["run_id"] != run["run_id"] or selection["run_id"] != run["run_id"]:
            raise ContractError("inputs.run_id", "run identity mismatch")
        if selection["trial_id"] != run["trial_id"]:
            raise ContractError("inputs.documents.selection", "trial identity mismatch")
        for name in ("config", "bundle"):
            if selection[name + "_sha256"] != data["documents"][name]["sha256"]:
                raise ContractError("inputs.documents.selection", "frozen content hash mismatch")
        source = data["configuration_source"]
        if (source is None) != (data["configuration_source_missing_reason"] is not None):
            raise ContractError("inputs.configuration_source", "missing source requires a reason")
        if source is not None:
            try:
                raw = base64.b64decode(source["content"], validate=True)
            except (ValueError, binascii.Error) as exc:
                raise ContractError("inputs.configuration_source", "invalid base64") from exc
            if len(raw) != source["bytes"] or hashlib.sha256(raw).hexdigest() != source["sha256"]:
                raise ContractError("inputs.configuration_source", "source bytes hash mismatch")
    elif kind == "observation_record":
        if content_digest(data["payload"]) != data["payload_sha256"]:
            raise ContractError("observation_record.payload_sha256", "content hash mismatch")
        if data["kind"] == "native_clean_receipt" and (
            data["payload"].get("request_id") != data["key"]
            or data["payload"].get("run_id") != data["run_id"]
        ):
            raise ContractError("observation_record.key", "receipt identity mismatch")
    elif kind == "prepared_request":
        from inferyard.contracts.schemas_events import MESSAGES

        if "messages" not in data["body"]:
            raise ContractError("prepared_request.body.messages", "original messages required")
        _validate(data["body"]["messages"], MESSAGES, "prepared_request.body.messages")
        if content_digest(data["body"]) != data["body_sha256"]:
            raise ContractError("prepared_request.body_sha256", "content hash mismatch")
