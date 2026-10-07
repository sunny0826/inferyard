"""Explicit, source-preserving migration into the sole active evidence contract."""

from __future__ import annotations

from datetime import UTC, datetime
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.contracts.validation import validate_document
from inferyard.evidence.migration_source import (
    digest,
    document,
    load_source,
    manifest_document,
    safe_name,
)
from inferyard.evidence.migration_transform import LIMITATIONS, transform
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
    sha256_file,
)

DEFINITION = "legacy-run-migration.v1"
RECEIPT_KEYS = {
    "schema_version",
    "definition",
    "source_path",
    "source_schema_version",
    "source_run_id",
    "source_manifest_sha256",
    "created_at",
    "files",
    "converted_files",
    "limitations",
}


def _receipt(root):
    proof = read_json(local_file(root, "migration.json"))
    if (
        type(proof) is not dict
        or set(proof) != RECEIPT_KEYS
        or type(proof["schema_version"]) is not int
        or proof["schema_version"] != SCHEMA_VERSION
        or proof["definition"] != DEFINITION
        or type(proof["source_schema_version"]) is not int
        or proof["source_schema_version"] not in (1, 2)
        or type(proof["source_path"]) is not str
        or type(proof["source_run_id"]) is not str
        or type(proof["created_at"]) is not str
        or type(proof["files"]) is not dict
        or type(proof["converted_files"]) is not dict
        or proof["limitations"] != LIMITATIONS
    ):
        raise EvidenceError("invalid_migration_receipt")
    return proof


def verify_migrated_run(root: Path) -> None:
    """Verify archived bytes and the deterministic mapping, without entering the ledger."""
    root = Path(root)
    proof = _receipt(root)
    raw_manifest = local_file(root, "source/manifest.json").read_bytes()
    if digest(raw_manifest) != proof["source_manifest_sha256"]:
        raise EvidenceError("migration_source_manifest_hash_mismatch")
    manifest = manifest_document(raw_manifest)
    if set(proof["files"]) != set(manifest["files"]):
        raise EvidenceError("migration_source_inventory_mismatch")
    blobs = {}
    for name, entry in manifest["files"].items():
        record = proof["files"][name]
        if (
            type(record) is not dict
            or set(record) != {"archive", "output"}
            or record["archive"] != f"source/{entry['sha256']}.bin"
        ):
            raise EvidenceError("invalid_migration_archive_reference")
        raw = local_file(root, record["archive"]).read_bytes()
        if digest(raw) != entry["sha256"] or len(raw) != entry["bytes"]:
            raise EvidenceError("migration_archived_source_hash_mismatch")
        blobs[name] = raw
    original = document(blobs["run.json"])
    if (
        original["run_id"] != proof["source_run_id"]
        or original["schema_version"] != proof["source_schema_version"]
    ):
        raise EvidenceError("migration_source_identity_mismatch")
    expected = transform(manifest, blobs)
    if proof["converted_files"] != {name: digest(raw) for name, raw in expected.items()}:
        raise EvidenceError("migration_conversion_inventory_mismatch")
    for name, raw in expected.items():
        if local_file(root, name).read_bytes() != raw:
            raise EvidenceError("migration_conversion_differs_from_source")
    copied = set()
    for name, record in proof["files"].items():
        output = record["output"]
        if output is None:
            continue
        safe_name(output)
        if output in expected or output.startswith("source/") or output in copied:
            raise EvidenceError("migration_copy_overlaps_generated_evidence")
        copied.add(output)
        if local_file(root, output).read_bytes() != blobs[name]:
            raise EvidenceError("migration_copy_differs_from_source")


def migrate_run(source: Path, out: Path) -> dict:
    """Create one standalone current package; never overwrite or mutate its source."""
    source, out = Path(source).resolve(), Path(out).absolute()
    if out.resolve().is_relative_to(source) or source.is_relative_to(out.resolve()):
        raise EvidenceError("migration_output_overlaps_source")
    if out.exists():
        raise EvidenceError("migration_output_must_be_new")
    raw_manifest, manifest, blobs, physical_names = load_source(source)
    converted = transform(manifest, blobs)
    source_run = document(blobs["run.json"])
    proof = {
        "schema_version": SCHEMA_VERSION,
        "definition": DEFINITION,
        "source_path": str(source),
        "source_schema_version": source_run["schema_version"],
        "source_run_id": source_run["run_id"],
        "source_manifest_sha256": digest(raw_manifest),
        "created_at": datetime.now(UTC).isoformat(),
        "files": {},
        "converted_files": {name: digest(raw) for name, raw in converted.items()},
        "limitations": LIMITATIONS,
    }
    payloads = {**converted, "source/manifest.json": raw_manifest}
    for name, raw in blobs.items():
        archive = f"source/{digest(raw)}.bin"
        payloads[archive] = raw
        output = None
        if name not in converted and not manifest["files"][name]["derived"]:
            output = physical_names[name]
            if output.startswith("source/") or output in {"manifest.json", "migration.json"}:
                raise EvidenceError("legacy_source_uses_migration_reserved_path")
            if output in payloads:
                raise EvidenceError("migration_output_path_collision")
            payloads[output] = raw
        proof["files"][name] = {"archive": archive, "output": output}
    payloads["migration.json"] = json_bytes(proof)
    out.mkdir(parents=True, exist_ok=False)
    for name, raw in payloads.items():
        target = out / name
        target.parent.mkdir(parents=True, exist_ok=True)
        atomic_bytes(target, raw)
    verify_migrated_run(out)
    seal = {
        "schema_version": SCHEMA_VERSION,
        "run_id": source_run["run_id"],
        "sealed": True,
        "files": {
            name: {"sha256": digest(raw), "bytes": len(raw), "derived": False}
            for name, raw in sorted(payloads.items())
        },
    }
    validate_document("manifest", seal)
    atomic_bytes(out / "manifest.json", json_bytes(seal))
    # The same active reader must accept this package before migration is complete.
    from inferyard.evidence.ledger import read_trial

    data = read_trial(out)
    return {
        "run_id": data["run"]["run_id"],
        "source_schema_version": source_run["schema_version"],
        "schema_version": SCHEMA_VERSION,
        "source_manifest_sha256": proof["source_manifest_sha256"],
        "manifest_sha256": sha256_file(out / "manifest.json"),
        "output": str(out.resolve()),
        "origin": "migrated",
        "completeness": data["summary"]["completeness"],
        "limitations": LIMITATIONS,
    }
