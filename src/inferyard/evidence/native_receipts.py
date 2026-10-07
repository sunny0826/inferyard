"""Optional native client-completion receipts; these never establish engine-wide drain."""

import hashlib
import re

from inferyard.evidence.storage import EvidenceError, local_file, read_json


def clean_receipt_name(key):
    return "client-http-clean-" + hashlib.sha256(key.encode()).hexdigest() + ".json"


def verify_clean_receipts(root, events, client_ids, manifest):
    completed = {
        event["request_id"]: event
        for event in events
        if event["event_type"] == "request_finished"
        and event["data"]["execution_state"] == "completed"
    }
    expected_names = {clean_receipt_name(key) for key in completed}
    actual = {path.name for path in root.glob("client-http-clean-*.json")}
    sealed = {name for name in manifest or {} if name.startswith("client-http-clean-")}
    if actual - expected_names or sealed - actual:
        raise EvidenceError("native_clean_receipt_inventory_mismatch")
    for key, event in completed.items():
        name = clean_receipt_name(key)
        if name not in actual:
            continue  # Pre-receipt v3 records retain their reading semantics.
        if manifest is not None and name not in manifest:
            raise EvidenceError("native_clean_receipt_unsealed")
        receipt = read_json(local_file(root, name))
        if type(receipt) is not dict:
            raise EvidenceError("native_clean_receipt_binding_mismatch")
        expected = {
            "run_id": event["run_id"],
            "request_id": key,
            "client_request_id": client_ids[key]["id"],
            "dirty_token": receipt.get("dirty_token"),
            "completion_scope": "client_http",
            "basis": "validated_http_response_and_closed_context",
            "finish_reason": event["data"]["raw_finish_reason"],
            "engine_internal_drain": None,
            "missing_reason": "native_lifecycle_unavailable",
        }
        token = receipt.get("dirty_token")
        if (
            receipt != expected
            or type(token) is not str
            or not re.fullmatch(r"[a-f0-9]{32}", token)
        ):
            raise EvidenceError("native_clean_receipt_binding_mismatch")
