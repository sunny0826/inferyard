"""Native client-completion receipts; only legacy layouts may omit them.

Client HTTP completion never establishes engine-wide drain.
"""

import hashlib
import re

from inferyard.evidence.engine_capabilities import observation_sealed
from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_evidence import TrialEvidence


def clean_receipt_name(key):
    return "client-http-clean-" + hashlib.sha256(key.encode()).hexdigest() + ".json"


def verify_clean_receipts(root, events, client_ids, manifest, *, reads=None, allow_pending=False):
    reads = reads if reads is not None else TrialEvidence(root)
    completed = {
        event["request_id"]: event
        for event in events
        if event["event_type"] == "request_finished"
        and event["data"]["execution_state"] == "completed"
    }
    actual = set(reads.observation_keys("native_clean_receipt", stage="client_complete"))
    if actual - completed.keys():
        raise EvidenceError("native_clean_receipt_inventory_mismatch")
    for key, event in completed.items():
        if key not in actual:
            if reads.layout_version == 1 or allow_pending:
                continue  # Pre-receipt v3 records retain their reading semantics.
            raise EvidenceError("native_clean_receipt_missing")
        if manifest is not None and not observation_sealed(
            reads, "native_clean_receipt", manifest, stage="client_complete", key=key
        ):
            raise EvidenceError("native_clean_receipt_unsealed")
        receipt = reads.observation("native_clean_receipt", stage="client_complete", key=key)
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
