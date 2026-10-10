"""Closed durable observation slots and prepared request identities for run writers."""

import hashlib
import time
from datetime import UTC, datetime

from inferyard import SCHEMA_VERSION
from inferyard.contracts.validation import ContractError, validate_document
from inferyard.evidence.observations import MULTI_STAGE, validate_query
from inferyard.evidence.storage import EvidenceError, json_bytes


class RunRecords:
    def _initialize_records(self, config):
        self._record_adapter = config["engine"]["adapter"]
        self._observation_slots = {}
        self._observation_seq = 0
        self._prepared_requests = {}
        self._prepared_ordinal = 0

    def observe(self, kind, payload, *, stage, key=None):
        self._ensure_writable()
        cleaned = self.redactor.clean(payload)
        try:
            row = {
                "schema_version": SCHEMA_VERSION,
                "evidence_layout_version": 2,
                "run_id": self.run_id,
                "trial_id": self.trial_id,
                "seq": self._observation_seq + 1,
                "kind": kind,
                "stage": stage,
                "key": key,
                "clock_id": self.clock_id,
                "monotonic_ns": time.monotonic_ns(),
                "utc": datetime.now(UTC).isoformat(),
                "payload": cleaned,
                "payload_sha256": hashlib.sha256(json_bytes(cleaned)).hexdigest(),
            }
            validate_document("observation_record", row)
        except (ValueError, ContractError) as exc:
            raise EvidenceError("invalid_observation_record") from exc
        if kind == "service_properties":
            expected = "pre_idle" if self._record_adapter in ("kvmem", "ninfer") else "post_idle"
            if stage != expected:
                raise EvidenceError("service_properties_stage_mismatch")
        slot = (kind, stage if kind in MULTI_STAGE else None, key)
        if slot in self._observation_slots:
            raise EvidenceError("duplicate_observation_slot")
        self._publish_observation(row)
        self._observation_slots[slot] = stage
        self._observation_seq = row["seq"]

    def has_observation(self, kind, *, stage=None, key=None):
        validate_query(kind, stage, key)
        slot = (kind, stage if kind in MULTI_STAGE else None, key)
        if stage is None and kind in MULTI_STAGE:
            return any(saved[0] == kind and saved[2] == key for saved in self._observation_slots)
        return slot in self._observation_slots and (
            stage is None or self._observation_slots[slot] == stage
        )

    def prepare_request(self, request_id, phase, ordinal, body):
        self._ensure_writable()
        cleaned = self.redactor.clean(body)
        try:
            row = {
                "schema_version": SCHEMA_VERSION,
                "evidence_layout_version": 2,
                "run_id": self.run_id,
                "trial_id": self.trial_id,
                "request_id": request_id,
                "phase": phase,
                "ordinal": ordinal,
                "body": cleaned,
                "body_sha256": hashlib.sha256(json_bytes(cleaned)).hexdigest(),
            }
            validate_document("prepared_request", row)
        except (ValueError, ContractError) as exc:
            raise EvidenceError("invalid_prepared_request") from exc
        if request_id in self._prepared_requests or ordinal <= self._prepared_ordinal:
            raise EvidenceError("duplicate_or_misordered_prepared_request")
        self._publish_prepared(row)
        self._prepared_requests[request_id] = (
            phase,
            ordinal,
            hashlib.sha256(json_bytes(cleaned["messages"])).hexdigest(),
        )
        self._prepared_ordinal = ordinal

    def _check_prepared_start(self, phase, request_id, body):
        prepared = self._prepared_requests.get(request_id)
        if prepared is None:
            raise EvidenceError("request_snapshot_missing")
        if prepared[0] != phase:
            raise EvidenceError("request_body_identity_mismatch")
        digest = body.get("messages_sha256")
        if digest is None and "messages" in body:
            digest = hashlib.sha256(json_bytes(self.redactor.clean(body["messages"]))).hexdigest()
        if prepared[2] != digest:
            raise EvidenceError("request_messages_hash_mismatch")
