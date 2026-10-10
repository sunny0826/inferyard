"""Single-pass prepared request index; bodies are re-read only by byte range."""

import hashlib

from inferyard.contracts.validation import ContractError, strict_json_loads, validate_document
from inferyard.evidence.storage import EvidenceError, local_file


class PreparedRequests:
    def __init__(self, evidence):
        self.evidence = evidence
        self.entries = None
        self.tails = []

    def scan(self):
        if self.entries is not None:
            return
        entries, ordinals = {}, set()
        digest, size, previous = hashlib.sha256(), 0, 0
        try:
            with local_file(self.evidence.root, "request-bodies.jsonl").open("rb") as stream:
                for line in stream:
                    digest.update(line)
                    length = len(line)
                    if not line.endswith(b"\n"):
                        self.tails.append(f"truncated_tail_at_byte:{size}")
                    else:
                        record = strict_json_loads(line.decode("utf-8"))
                        validate_document("prepared_request", record)
                        self.evidence._check_identity(record)
                        key, ordinal = record["request_id"], record["ordinal"]
                        if key in entries or ordinal in ordinals or ordinal <= previous:
                            raise EvidenceError("duplicate_or_misordered_prepared_request")
                        entries[key] = (
                            size,
                            length,
                            hashlib.sha256(line).hexdigest(),
                            record["phase"],
                            record["body_sha256"],
                        )
                        previous = ordinal
                        ordinals.add(ordinal)
                    size += length
        except (OSError, UnicodeError, ContractError) as exc:
            raise EvidenceError("invalid_prepared_request") from exc
        self.evidence.container_tails["request-bodies.jsonl"] = self.tails
        self.evidence.hashes["request-bodies.jsonl"] = digest.hexdigest()
        self.evidence.sizes["request-bodies.jsonl"] = size
        if self.tails and self.evidence.manifest is not None:
            raise EvidenceError("sealed_jsonl_truncated")
        self.evidence._check_sealed("request-bodies.jsonl")
        self.entries = entries

    def record(self, event):
        self.scan()
        self.evidence._check_identity(event)
        entry = self.entries.get(event["request_id"])
        if entry is None:
            raise EvidenceError("request_snapshot_missing")
        offset, length, row_digest, phase, _ = entry
        if event["phase"] != phase:
            raise EvidenceError("request_body_identity_mismatch")
        try:
            with local_file(self.evidence.root, "request-bodies.jsonl").open("rb") as stream:
                if stream.seek(0, 2) != self.evidence.sizes["request-bodies.jsonl"]:
                    raise EvidenceError("request_body_changed")
                stream.seek(offset)
                line = stream.read(length)
        except OSError as exc:
            raise EvidenceError("request_body_changed") from exc
        if hashlib.sha256(line).hexdigest() != row_digest:
            raise EvidenceError("request_body_changed")
        return strict_json_loads(line.decode("utf-8"))
