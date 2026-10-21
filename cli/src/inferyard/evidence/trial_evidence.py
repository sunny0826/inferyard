"""Domain view of physical run originals, with a private read-only layout 1 adapter."""

import base64
from pathlib import Path

from inferyard.contracts.engine_profiles import observes_before_idle
from inferyard.contracts.schemas_layout import INPUT_KINDS, OBSERVATION_STAGES
from inferyard.contracts.validation import ContractError, validate_document
from inferyard.evidence.formats import UnsupportedFormat, require_core
from inferyard.evidence.layout import INPUT_FILES, OBSERVATION_FILES, identify
from inferyard.evidence.observations import Observations, validate_query
from inferyard.evidence.prepared import PreparedRequests
from inferyard.evidence.reference import verify_reference as verify_reference
from inferyard.evidence.storage import EvidenceError, verify_manifest_files
from inferyard.evidence.trial_reads import TrialReads


class TrialEvidence:
    def __init__(self, root, projection=False):
        self.root = Path(root).resolve()
        self.projection = projection
        self._reads = TrialReads(self.root)
        self.manifest = self._reads.manifest
        if "manifest.json" in self._reads.errors:
            raise self._reads.errors["manifest.json"]
        if self.manifest is not None and (
            type(self.manifest) is not dict or type(self.manifest.get("files")) is not dict
        ):
            raise EvidenceError("invalid_manifest")
        self.hashes, self.sizes, self.errors = (
            self._reads.hashes,
            self._reads.sizes,
            self._reads.errors,
        )
        try:
            self.layout_version = identify(self._reads)
            if self.manifest is not None:
                require_core(self.manifest, "manifest")
        except UnsupportedFormat:
            if self.manifest is not None:
                verify_manifest_files(self.root, self.manifest, observed=self._reads.observed())
            raise
        if self.manifest is not None:
            try:
                validate_document("manifest", self.manifest)
            except ContractError as exc:
                raise EvidenceError("invalid_manifest") from exc
        self._inputs = None
        if self.layout_version == 2:
            self._inputs = self._reads.checked_json("inputs.json")
            try:
                validate_document("inputs", self._inputs)
            except ContractError as exc:
                raise EvidenceError("invalid_inputs_container") from exc
            for member in self._inputs["documents"].values():
                require_core(member["payload"], "run.input")
            self._check_sealed("inputs.json")
        self._container_tails = {}
        self._observations = Observations(self)
        self._prepared = PreparedRequests(self)

    @property
    def container_tails(self):
        return {name: list(tails) for name, tails in self._container_tails.items()}

    def exists(self, name):
        """Physical inventory query; logical members use has_observation/input."""
        return self._reads.exists(name)

    def input(self, kind):
        if kind not in INPUT_KINDS:
            raise EvidenceError("invalid_input_kind")
        if self.layout_version == 2:
            return self._inputs["documents"][kind]["payload"]
        return self._reads.checked_json(INPUT_FILES[kind])

    def input_digest(self, kind):
        self.input(kind)
        if self.layout_version == 2:
            return self._inputs["documents"][kind]["sha256"]
        return self.hashes[INPUT_FILES[kind]]

    def input_reference(self, kind):
        if self.layout_version == 2:
            return self._reference(
                "inputs.json",
                {
                    "kind": "input",
                    "name": kind,
                    "sha256": self.input_digest(kind),
                },
            )
        self.input(kind)
        return self._reference(INPUT_FILES[kind])

    def configuration_source(self):
        if self.layout_version == 2:
            source = self._inputs["configuration_source"]
            return {
                "media_type": source["media_type"] if source else None,
                "data": base64.b64decode(source["content"]) if source else None,
                "missing_reason": self._inputs["configuration_source_missing_reason"],
            }
        names = [name for name in ("config.input.json", "config.input.toml") if self.exists(name)]
        if len(names) > 1:
            raise EvidenceError("configuration_source_conflict")
        if not names:
            return {"media_type": None, "data": None, "missing_reason": "not_persisted"}
        name = names[0]
        raw = self._reads.bytes(name)
        self._check_sealed(name)
        return {
            "media_type": "application/toml" if name.endswith(".toml") else "application/json",
            "data": raw,
            "missing_reason": None,
        }

    def _legacy_observation_name(self, kind, stage, key):
        validate_query(kind, stage, key)
        if kind == "native_clean_receipt":
            from inferyard.evidence.native_receipts import clean_receipt_name

            return clean_receipt_name(key)
        if stage is None and len(OBSERVATION_STAGES[kind]) == 1:
            stage = OBSERVATION_STAGES[kind][0]
        if kind == "service_properties":
            actual = (
                "pre_idle"
                if observes_before_idle(self.input("config")["engine"]["adapter"])
                else "post_idle"
            )
            if stage is not None and stage != actual:
                return None
            stage = None
        elif kind == "identity":
            stage = None  # Legacy identity has one authoritative slot without stage metadata.
        name = OBSERVATION_FILES.get((kind, stage))
        if name is None:
            raise EvidenceError("ambiguous_observation_query")
        return name

    def observation(self, kind, stage=None, key=None):
        if self.layout_version == 2:
            record = self._observations.get(kind, stage, key)
            return record["payload"] if record else None
        name = self._legacy_observation_name(kind, stage, key)
        if name is None or not self.exists(name):
            return None
        payload = self._reads.checked_json(name)
        self._check_sealed(name)
        return payload

    def has_observation(self, kind, stage=None, key=None):
        return self.observation(kind, stage, key) is not None

    def observation_keys(self, kind, stage=None):
        validate_query(kind, stage, "query" if kind == "native_clean_receipt" else None)
        if self.layout_version == 2:
            self._observations.scan()
            return [
                row["key"]
                for row in self._observations.records
                if row["kind"] == kind and (stage is None or row["stage"] == stage)
            ]
        if kind != "native_clean_receipt":
            stages = (stage,) if stage is not None else OBSERVATION_STAGES[kind]
            if kind in ("identity", "service_properties"):
                stages = (stage,)
            return [None for candidate in stages if self.has_observation(kind, candidate)]
        from inferyard.evidence.native_receipts import clean_receipt_name

        names = {path.name for path in self.root.glob("client-http-clean-*.json")}
        if self.manifest is not None:
            sealed = {
                name for name in self.manifest["files"] if name.startswith("client-http-clean-")
            }
            if sealed != names:
                raise EvidenceError("native_clean_receipt_inventory_mismatch")
        keys = []
        for name in sorted(names):
            receipt = self._reads.checked_json(name)
            key = receipt.get("request_id") if type(receipt) is dict else None
            if not isinstance(key, str) or not key or clean_receipt_name(key) != name:
                raise EvidenceError("native_clean_receipt_binding_mismatch")
            if receipt.get("run_id") != self.input("run")["run_id"]:
                raise EvidenceError("native_clean_receipt_binding_mismatch")
            self._check_sealed(name)
            keys.append(key)
        return keys

    def observation_reference(self, kind, stage=None, key=None):
        if self.layout_version == 2:
            row = self._observations.get(kind, stage, key)
            if row is None:
                return None
            return self._reference(
                "observations.jsonl",
                {
                    "kind": "observation",
                    "seq": row["seq"],
                    "sha256": row["payload_sha256"],
                },
            )
        if self.observation(kind, stage, key) is None:
            return None
        return self._reference(self._legacy_observation_name(kind, stage, key))

    def _legacy_request_name(self, event):
        from inferyard.evidence.request_snapshots import snapshot_name_for_event

        name = snapshot_name_for_event(self.root, event)
        if not self.exists(name):
            raise EvidenceError("request_snapshot_missing")
        if self.manifest is not None and name not in self.manifest["files"]:
            raise EvidenceError("request_snapshot_unsealed")
        return name

    def request_body(self, event):
        if self.layout_version == 2:
            return self._prepared.record(event)["body"]
        self._check_identity(event)
        name = self._legacy_request_name(event)
        body = self._reads.checked_json(name)
        self._check_sealed(name)
        return body

    def request_reference(self, event):
        if self.layout_version == 2:
            row = self._prepared.record(event)
            return self._reference(
                "request-bodies.jsonl",
                {
                    "kind": "request",
                    "request_id": row["request_id"],
                    "sha256": row["body_sha256"],
                },
            )
        self.request_body(event)
        return self._reference(self._legacy_request_name(event))

    def log_reference(self, name):
        if not name.endswith(".jsonl"):
            raise EvidenceError("invalid_log_reference")
        if name == "request-bodies.jsonl" and self.layout_version == 2:
            self._prepared.scan()
        else:
            self.checked_jsonl(name)
        return self._reference(name)

    def _reference(self, name, member=None):
        ref = {"path": name, "sha256": self.hashes[name]}
        if member is not None:
            ref["member"] = member
        return ref

    def _check_identity(self, row):
        run = self.input("run")
        if row.get("run_id") != run["run_id"] or row.get("trial_id") != run["trial_id"]:
            raise EvidenceError("evidence_record_identity_mismatch")

    def _check_sealed(self, name):
        if self.manifest is None or self.projection:
            return
        info = self.manifest["files"].get(name)
        if info is None:
            raise EvidenceError("original_evidence_unsealed")
        if info.get("sha256") != self.hashes.get(name) or info.get("bytes") != self.sizes.get(name):
            raise EvidenceError("original_evidence_hash_mismatch")

    def jsonl(self, name):
        if name == "request-bodies.jsonl" and self.layout_version == 2:
            raise EvidenceError("prepared_requests_require_identity_query")
        if self.layout_version == 2 and self.manifest is None and not self.exists(name):
            return [], []
        return self._reads.jsonl(name)

    def checked_jsonl(self, name):
        if name == "request-bodies.jsonl" and self.layout_version == 2:
            raise EvidenceError("prepared_requests_require_identity_query")
        records, tails = self._reads.checked_jsonl(name)
        if tails and self.manifest is not None and self.layout_version == 2:
            raise EvidenceError("sealed_jsonl_truncated")
        return records, tails

    def observed(self):
        return self._reads.observed()

    def check_input_errors(self):
        self._reads.check_input_errors()

    def preload(self, manifest_files):
        if self.layout_version == 1:
            self._reads.preload(manifest_files)
        elif not self.projection:
            self._observations.scan()
            self._prepared.scan()
            for name in ("environment.jsonl", "schedule.jsonl", "external-cpu.jsonl"):
                if name in manifest_files:
                    self.jsonl(name)
