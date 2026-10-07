"""Portable request body filenames and sealed legacy snapshot resolution."""

import re
from ntpath import isreserved
from pathlib import Path

from inferyard.evidence.storage import EvidenceError, local_file, read_json


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


def legacy_snapshot_filename(name, run_id=None):
    """Map a historical leaf name; the run ID for v1 names comes from run.json."""
    match = re.fullmatch(
        r"([A-Za-z0-9][A-Za-z0-9._-]*)(?P<sep>[:-])(probe|warmup|formal)(?P=sep)"
        r"([1-9][0-9]*)\.request\.json",
        name,
    )
    if match:
        return snapshot_filename(match[1], match[3], int(match[4]))
    match = re.fullmatch(r"(probe|warmup|formal)-([1-9][0-9]*)\.request\.json", name)
    if match and run_id is not None:
        return snapshot_filename(run_id, match[1], int(match[2]))
    return None


def migrated_snapshot_path(root, relative):
    """Resolve renamed bodies while preserving original manifest and provenance bytes."""
    from inferyard.platforms.platform_io import legacy_request_alias

    path = root / relative
    parent, leaf = path.parent, path.name
    mapped = legacy_snapshot_filename(leaf)
    if mapped is not None:
        return parent / mapped
    if re.fullmatch(r"(probe|warmup|formal)-[1-9][0-9]*\.request\.json", leaf):
        run_path = local_file(root, (parent / "run.json").relative_to(root).as_posix())
        if run_path.is_file():
            run = read_json(run_path)
            if not isinstance(run, dict):
                raise EvidenceError("invalid_json_evidence")
            mapped = legacy_snapshot_filename(leaf, run.get("run_id"))
            if mapped is not None:
                return parent / mapped
    if parent.name == "request-files" and re.fullmatch(r"[0-9a-f]{64}\.request\.json", leaf):
        packet = parent.parent
        manifest_path = local_file(root, (packet / "manifest.json").relative_to(root).as_posix())
        if manifest_path.is_file():
            manifest = read_json(manifest_path)
            run_id = manifest.get("run_id")
            for original in manifest.get("files", {}):
                if legacy_request_alias(original) == "request-files/" + leaf:
                    mapped = legacy_snapshot_filename(original, run_id)
                    if mapped is not None:
                        return packet / mapped
    return None


def snapshot_name_for_event(root, event, manifest=None):
    """Prefer canonical names; sealed reads choose only a manifest-bound candidate.

    Request IDs remain event identities. Older runs used phase-N, run-phase-N or
    run:phase:N filenames, including archived Windows aliases resolved by local_file.
    """
    request_id, run_id, phase = event["request_id"], event["run_id"], event["phase"]
    legacy = request_id + ".request.json"
    prefix = re.escape(run_id)
    stage = re.escape(phase)
    match = re.fullmatch(
        rf"(?:{prefix}(?P<separator>[:-]){stage}(?P=separator)|{stage}-)([1-9][0-9]*)",
        request_id,
    )
    names = [legacy]
    if match:
        names.insert(0, snapshot_filename(run_id, phase, int(match[2])))
    root = Path(root)
    for name in names:
        if manifest is not None:
            if name in manifest:
                return name
        else:
            try:
                if local_file(root, name).is_file():
                    return name
            except EvidenceError as exc:
                if str(exc) != "portable_legacy_request_missing":
                    raise
    # Let protocol readers retain their precise missing/unsealed error categories.
    return names[0]
