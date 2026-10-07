"""Bounded legacy input inspection, used only by explicit evidence migration."""

from __future__ import annotations

import hashlib
import re
from pathlib import Path, PurePosixPath, PureWindowsPath

from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.evidence.storage import EvidenceError, local_file

CORE_FILES = frozenset(
    ("run.json", "plan.json", "config.frozen.json", "bundle.json", "events.jsonl", "memory.jsonl")
)
HASH = re.compile(r"[a-f0-9]{64}")


def digest(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


def wire_version(data, expected):
    return type(data.get("schema_version")) is int and data["schema_version"] == expected


def document(raw: bytes):
    try:
        return strict_json_loads(raw.decode("utf-8"))
    except (UnicodeError, ContractError) as exc:
        raise EvidenceError("invalid_migration_source_json") from exc


def records(raw: bytes) -> list[dict]:
    """Sealed inputs must contain complete records; no silently dropped tails."""
    if raw and not raw.endswith(b"\n"):
        raise EvidenceError("migration_requires_complete_jsonl_records")
    result = [document(line) for line in raw.splitlines()]
    if any(type(item) is not dict for item in result):
        raise EvidenceError("invalid_migration_source_record")
    return result


def safe_name(name: str) -> None:
    # Historic request names can contain colons, but never actual drive prefixes.
    if (
        type(name) is not str
        or not name
        or "\\" in name
        or PurePosixPath(name).is_absolute()
        or PureWindowsPath(name).drive
        or any(part in ("", ".", "..") for part in name.split("/"))
    ):
        raise EvidenceError("unsafe_migration_source_path")


def manifest_document(raw: bytes) -> dict:
    manifest = document(raw)
    if (
        type(manifest) is not dict
        or set(manifest) != {"schema_version", "sealed", "run_id", "files"}
        or type(manifest["schema_version"]) is not int
        or manifest["schema_version"] not in (1, 2)
        or manifest["sealed"] is not True
        or type(manifest["run_id"]) is not str
        or type(manifest["files"]) is not dict
        or not CORE_FILES.issubset(manifest["files"])
    ):
        raise EvidenceError("migration_requires_sealed_legacy_run")
    for name, entry in manifest["files"].items():
        safe_name(name)
        if (
            type(entry) is not dict
            or set(entry) != {"sha256", "bytes", "derived"}
            or type(entry["sha256"]) is not str
            or not HASH.fullmatch(entry["sha256"])
            or type(entry["bytes"]) is not int
            or entry["bytes"] < 0
            or type(entry["derived"]) is not bool
        ):
            raise EvidenceError("invalid_legacy_manifest_entry")
    return manifest


def source_documents(blobs: dict[str, bytes], manifest: dict) -> dict:
    """Check frozen bindings before any representation changes are permitted."""
    documents = {
        name: document(blobs[name + ".json"]) for name in ("run", "plan", "config.frozen", "bundle")
    }
    run = documents["run"]
    version = run.get("schema_version")
    if type(version) is not int or version not in (1, 2):
        raise EvidenceError("unsupported_legacy_run_version")
    # Historical TrialJournal v2 reused EvidenceStore's v1 manifest envelope.
    # The manifest revision therefore need not equal the run revision.
    if run.get("run_id") != manifest["run_id"]:
        raise EvidenceError("migration_source_run_identity_mismatch")
    if not wire_version(documents["config.frozen"], 1):
        raise EvidenceError("unsupported_legacy_config_version")
    if version == 1:
        if (
            not wire_version(documents["bundle"], 1)
            or not wire_version(documents["plan"], 1)
            or documents["plan"].get("run_id") != run["run_id"]
            or run.get("bundle_sha256") != digest(blobs["bundle.json"])
        ):
            raise EvidenceError("legacy_single_input_binding_mismatch")
    else:
        if "selection.json" not in blobs:
            raise EvidenceError("legacy_selection_missing")
        selection = document(blobs["selection.json"])
        documents["selection"] = selection
        if (
            not wire_version(selection, 2)
            or not wire_version(documents["plan"], 2)
            or not wire_version(documents["plan"]["experiment"], 2)
            or selection.get("run_id") != run["run_id"]
            or selection.get("trial_id") != run.get("trial_id")
            or selection.get("bundle_sha256") != digest(blobs["bundle.json"])
            or selection.get("config_sha256") != digest(blobs["config.frozen.json"])
            or run.get("plan_sha256") != documents["plan"].get("plan_sha256")
        ):
            raise EvidenceError("legacy_trial_input_binding_mismatch")
        # The plan hash algorithm is independent of the wire version.
        from inferyard.config.plan_math import plan_hash

        if plan_hash(documents["plan"]) != documents["plan"]["plan_sha256"]:
            raise EvidenceError("legacy_plan_hash_mismatch")
    return documents


def load_source(root: Path) -> tuple[bytes, dict, dict[str, bytes], dict[str, str]]:
    raw_manifest = local_file(root, "manifest.json").read_bytes()
    manifest = manifest_document(raw_manifest)
    blobs, physical_names = {}, {}
    for name, entry in manifest["files"].items():
        path = local_file(root, name)
        raw = path.read_bytes()
        if len(raw) != entry["bytes"] or digest(raw) != entry["sha256"]:
            raise EvidenceError("migration_source_hash_mismatch")
        physical = path.resolve().relative_to(root.resolve()).as_posix()
        safe_name(physical)
        blobs[name], physical_names[name] = raw, physical
    source_documents(blobs, manifest)
    return raw_manifest, manifest, blobs, physical_names
