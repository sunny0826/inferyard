"""Verify real physical bytes and optional container member bindings."""

from pathlib import Path

from inferyard.contracts.schemas_evidence_reference import EVIDENCE_REFERENCE
from inferyard.contracts.validation import validate_value
from inferyard.errors import ContractError, EvidenceError
from inferyard.evidence.storage import local_file, sha256_file


def verify_reference(root, ref, *, reads=None, readers=None):
    from inferyard.contracts.contracts_experiment import validate_artifact_reference
    from inferyard.evidence.trial_evidence import TrialEvidence

    try:
        validate_value(ref, EVIDENCE_REFERENCE, "evidence_reference")
        validate_artifact_reference(ref, "evidence_reference.path")
    except ContractError as exc:
        raise EvidenceError("invalid_evidence_reference") from exc
    path = local_file(Path(root).resolve(), ref["path"])
    if reads is not None and (reads.root != Path(root).resolve() or reads.projection):
        raise EvidenceError("invalid_evidence_reference")
    member = ref.get("member")
    if member is None:
        try:
            name = path.relative_to(reads.root).as_posix() if reads is not None else None
            digest = reads.hashes.get(name) if reads is not None else None
            digest = digest if digest is not None else sha256_file(path)
        except OSError as exc:
            raise EvidenceError("evidence_reference_missing") from exc
        if digest != ref["sha256"]:
            raise EvidenceError("evidence_reference_hash_mismatch")
        return
    expected_name = {
        "input": "inputs.json",
        "observation": "observations.jsonl",
        "request": "request-bodies.jsonl",
    }[member["kind"]]
    if path.name != expected_name:
        raise EvidenceError("evidence_reference_member_container_mismatch")
    if reads is not None and (reads.root != path.parent or reads.projection):
        raise EvidenceError("invalid_evidence_reference")
    evidence = reads
    if evidence is None:
        if readers is None:
            evidence = TrialEvidence(path.parent)
        else:
            if path.parent not in readers:
                readers[path.parent] = TrialEvidence(path.parent)
            evidence = readers[path.parent]
    if evidence.root != path.parent or evidence.projection:
        raise EvidenceError("invalid_evidence_reference")
    if evidence.layout_version != 2:
        raise EvidenceError("evidence_reference_member_container_mismatch")
    if member["kind"] == "input":
        actual = evidence.input_reference(member["name"])
    elif member["kind"] == "observation":
        evidence._observations.scan()
        records = evidence._observations.records
        row = records[member["seq"] - 1] if member["seq"] <= len(records) else None
        actual = None
        if row is not None:
            actual = evidence.observation_reference(row["kind"], row["stage"], row["key"])
    else:
        evidence._prepared.scan()
        entry = evidence._prepared.entries.get(member["request_id"])
        actual = None
        if entry:
            event = {
                **{key: evidence.input("run")[key] for key in ("run_id", "trial_id")},
                "request_id": member["request_id"],
                "phase": entry[3],
            }
            actual = evidence.request_reference(event)
    if actual is None or actual["member"] != member:
        raise EvidenceError("evidence_reference_member_mismatch")
    if actual["sha256"] != ref["sha256"]:
        raise EvidenceError("evidence_reference_hash_mismatch")


def file_reference(root, name):
    """Describe one saved source byte-for-byte under the evidence root."""
    return {"path": name, "sha256": sha256_file(local_file(root, name))}
