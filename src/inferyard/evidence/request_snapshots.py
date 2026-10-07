"""Portable request body filenames and explicit historical-source rejection."""

import hashlib
import re
from ntpath import isreserved

from inferyard.evidence.storage import EvidenceError


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
