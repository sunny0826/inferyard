"""Closed observation slots and strict run/trial/sequence binding."""

from inferyard.contracts.engine_profiles import observes_before_idle
from inferyard.contracts.schemas_layout import OBSERVATION_RECORD, OBSERVATION_STAGES
from inferyard.contracts.validation import ContractError, _validate, validate_document
from inferyard.evidence.storage import EvidenceError

MULTI_STAGE = {"environment", "effective_parameters"}


def validate_query(kind, stage, key):
    if kind not in OBSERVATION_STAGES or (
        stage is not None and stage not in OBSERVATION_STAGES[kind]
    ):
        raise EvidenceError("invalid_observation_query")
    if kind == "native_clean_receipt":
        if not isinstance(key, str) or not key:
            raise EvidenceError("invalid_observation_query")
    elif key is not None:
        raise EvidenceError("invalid_observation_query")


class Observations:
    def __init__(self, evidence):
        self.evidence = evidence
        self.records = None
        self._index = {}
        self.tails = []

    def scan(self):
        if self.records is not None:
            return
        if self.evidence.manifest is None and not self.evidence.exists("observations.jsonl"):
            self.records = []
            return
        rows, self.tails = self.evidence.checked_jsonl("observations.jsonl")
        self.evidence._container_tails["observations.jsonl"] = self.tails
        slots, records = set(), []
        for seq, row in enumerate(rows, 1):
            try:
                if self.evidence.projection:
                    _validate(row, OBSERVATION_RECORD, "observation_record")
                else:
                    validate_document("observation_record", row)
            except ContractError as exc:
                raise EvidenceError("invalid_observation_record") from exc
            self.evidence._check_identity(row)
            if row["seq"] != seq:
                raise EvidenceError("observation_sequence_mismatch")
            kind, stage, key = row["kind"], row["stage"], row["key"]
            if kind == "service_properties":
                adapter = self.evidence.input("config")["engine"]["adapter"]
                expected_stage = "pre_idle" if observes_before_idle(adapter) else "post_idle"
                if stage != expected_stage:
                    raise EvidenceError("service_properties_stage_mismatch")
            slot = (kind, stage if kind in MULTI_STAGE else None, key)
            if slot in slots:
                raise EvidenceError("duplicate_observation_slot")
            slots.add(slot)
            records.append(row)
            self._index.setdefault((kind, key), {})[stage] = row
        self.evidence._check_sealed("observations.jsonl")
        self.records = records

    def get(self, kind, stage=None, key=None):
        validate_query(kind, stage, key)
        self.scan()
        group = self._index.get((kind, key), {})
        matches = (
            list(group.values()) if stage is None else [group[stage]] if stage in group else []
        )
        if len(matches) > 1:
            raise EvidenceError("ambiguous_observation_query")
        if matches and self.evidence.projection:
            try:
                validate_document("observation_record", matches[0])
            except ContractError as exc:
                raise EvidenceError("invalid_observation_record") from exc
        return matches[0] if matches else None
