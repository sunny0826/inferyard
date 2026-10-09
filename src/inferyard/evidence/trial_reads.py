"""One reduction's parsed inputs and hashes, obtained from the same bytes."""

import hashlib

from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.evidence.storage import EvidenceError, local_file

SIDECARS = {"environment.end.json", "environment.jsonl", "schedule.jsonl", "external-cpu.jsonl"}


class TrialReads:
    def __init__(self, root):
        self.root = root
        self.documents = {}
        self.hashes = {}
        self.sizes = {}
        self.errors = {}
        self.logs = {}
        self.record_errors = {}
        self.manifest = self.json("manifest.json") if self.exists("manifest.json") else None

    def exists(self, name):
        return local_file(self.root, name).exists()

    def json(self, name):
        if name not in self.documents:
            try:
                with local_file(self.root, name).open("rb") as stream:
                    raw = stream.read()
                self.hashes[name] = hashlib.sha256(raw).hexdigest()
                self.sizes[name] = len(raw)
                self.documents[name] = strict_json_loads(raw.decode("utf-8"))
            except OSError, UnicodeError, ContractError:
                self.documents[name] = None
                self.errors[name] = EvidenceError("invalid_json_evidence")
        return self.documents[name]

    def jsonl(self, name):
        # Retain parsed records only until the caller finishes this run; never cache raw logs.
        if name in self.logs:
            return self.logs[name]
        records, tails, digest, size = [], [], hashlib.sha256(), 0
        try:
            with local_file(self.root, name).open("rb") as stream:
                for line in stream:
                    digest.update(line)
                    if not line.endswith(b"\n"):
                        tails.append(f"truncated_tail_at_byte:{size}")
                    else:
                        try:
                            record = strict_json_loads(line.decode("utf-8"))
                            if not isinstance(record, dict):
                                raise EvidenceError("invalid_jsonl_record")
                            records.append(record)
                        except (UnicodeError, ContractError, EvidenceError) as exc:
                            self.record_errors.setdefault(name, exc)
                            self.errors[name] = EvidenceError("corrupt_jsonl_evidence")
                    size += len(line)
        except OSError, UnicodeError, ContractError:
            self.errors[name] = EvidenceError("corrupt_jsonl_evidence")
            return records, tails
        self.hashes[name], self.sizes[name] = digest.hexdigest(), size
        self.logs[name] = records, tails
        return records, tails

    def observed(self):
        return {name: (self.sizes[name], digest) for name, digest in self.hashes.items()}

    def check_input_errors(self):
        # Sidecar parse failures retain the original measurement-context error order.
        for name, error in self.errors.items():
            if name not in SIDECARS:
                raise error

    def checked_json(self, name):
        value = self.json(name)
        if name in self.errors:
            raise self.errors[name]
        return value

    def checked_jsonl(self, name):
        value = self.jsonl(name)
        if name in self.errors:
            error = self.record_errors.get(name)
            if isinstance(error, EvidenceError):
                raise error
            raise self.errors[name]
        return value

    def preload(self, manifest_files):
        for name in (
            "identity.json",
            "environment.start.json",
            "environment.end.json",
            "service.props.json",
            "collector.json",
            "token-budgets.json",
            "token-budgets.v2.json",
        ):
            if name in manifest_files:
                self.json(name)
        for name in ("environment.jsonl", "schedule.jsonl", "external-cpu.jsonl"):
            if name in manifest_files:
                self.jsonl(name)
