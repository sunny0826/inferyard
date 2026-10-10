"""Portable request body filenames and explicit historical-source rejection."""

import hashlib
import re
from ntpath import isreserved

from inferyard.evidence.storage import EvidenceError, json_bytes


def messages_sha256(messages):
    return hashlib.sha256(json_bytes(messages)).hexdigest()


def verify_message_snapshots(root, events, reads):
    """New hash-only events require original snapshots, never reconstructed prompts."""
    from inferyard.contracts.schemas_events import MESSAGES
    from inferyard.contracts.validation import ContractError, _validate

    manifest = reads.manifest["files"] if reads.manifest is not None else None
    for event in events:
        if event["event_type"] != "request_started":
            continue
        body = event["data"]["body"]
        if "messages_sha256" not in body:
            continue  # Old inline events retain their original reading policy.
        name = snapshot_name_for_event(root, event, manifest)
        if not reads.exists(name):
            raise EvidenceError("request_snapshot_missing")
        if manifest is not None and name not in manifest:
            raise EvidenceError("request_snapshot_unsealed")
        snapshot = reads.checked_json(name)
        if type(snapshot) is not dict or "messages" not in snapshot:
            raise EvidenceError("request_snapshot_messages_invalid")
        try:
            _validate(snapshot["messages"], MESSAGES, "request.messages")
        except ContractError as exc:
            raise EvidenceError("request_snapshot_messages_invalid") from exc
        if messages_sha256(snapshot["messages"]) != body["messages_sha256"]:
            raise EvidenceError("request_messages_hash_mismatch")


def snapshot_filename(run_id, phase, ordinal):
    """Keep only portable ASCII characters, with a sortable request ordinal."""
    if (
        not isinstance(run_id, str)
        or not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", run_id)
        or phase not in ("probe", "warmup", "formal")
        or type(ordinal) is not int
        or ordinal <= 0
    ):
        raise EvidenceError("invalid_request_snapshot_identity")
    name = f"{run_id}__{ordinal:06d}__{phase}.request.json"
    if isreserved(name):
        raise EvidenceError("invalid_request_snapshot_identity")
    if len(name.encode("ascii")) > 255:
        raise EvidenceError("request_snapshot_filename_too_long")
    return name


def snapshot_name_for_event(root, event, manifest=None):
    """Resolve current portable bodies from the unchanged event request identity."""
    request_id, run_id, phase = event["request_id"], event["run_id"], event["phase"]
    match = re.fullmatch(
        rf"{re.escape(run_id)}(?P<sep>[:-]){re.escape(phase)}(?P=sep)([1-9][0-9]*)",
        request_id,
    )
    if match is None:
        # Opaque schema-valid event IDs do not invent an original request body.
        return "unbound-" + hashlib.sha256(request_id.encode()).hexdigest() + ".request.json"
    return snapshot_filename(run_id, phase, int(match[2]))


def require_current_sources(root, manifest=None):
    from inferyard.evidence.formats import UnsupportedFormat
    from inferyard.evidence.storage import local_file, read_json

    names = set(manifest or {}) | {path.name for path in root.iterdir()}
    for name in names:
        leaf = name.rsplit("/", 1)[-1]
        if (
            name in ("migration.json", "requests.jsonl", "token-budgets.json")
            or (
                leaf.endswith(".request.json")
                and not re.fullmatch(
                    r"[A-Za-z0-9][A-Za-z0-9._-]*__[0-9]{6,}__(probe|warmup|formal)\.request\.json",
                    leaf,
                )
            )
            or name == "request-files"
        ):
            if name.endswith(".json") and local_file(root, name).exists():
                read_json(local_file(root, name))
            raise UnsupportedFormat("run.source", name, ("current original evidence",))
