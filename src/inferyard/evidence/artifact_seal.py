"""Exact byte seals for derived artifacts; semantic verification remains separate."""

import hashlib

from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
    sha256_file,
)


def seal(root, names):
    hashes = {name: sha256_file(local_file(root, name)) for name in names}
    atomic_bytes(
        root / "artifact-manifest.json",
        json_bytes(
            {
                "definition": "presentation-seal.v1",
                "files": hashes,
            }
        ),
    )


def read_sealed(root, names, *, retain=None):
    """Verify every member; retain only requested bytes (all members by default)."""
    names = tuple(names)
    retain = set(names) if retain is None else set(retain)
    if not retain <= set(names):
        raise EvidenceError("presentation_seal_invalid")
    saved = read_json(local_file(root, "artifact-manifest.json"))
    if (
        type(saved) is not dict
        or set(saved) != {"definition", "files"}
        or saved["definition"] != "presentation-seal.v1"
        or type(saved["files"]) is not dict
        or set(saved["files"]) != set(names)
    ):
        raise EvidenceError("presentation_seal_invalid")
    blobs, hashes = {}, {}
    for name in names:
        path = local_file(root, name)
        if name in retain:
            raw = path.read_bytes()
            blobs[name] = raw
            hashes[name] = hashlib.sha256(raw).hexdigest()
        else:
            hashes[name] = sha256_file(path)
    if saved["files"] != hashes:
        raise EvidenceError("presentation_bytes_changed")
    return blobs
