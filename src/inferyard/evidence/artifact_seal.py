"""Exact byte seals for derived artifacts; semantic verification remains separate."""

import hashlib

from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
)


def seal(root, names):
    blobs = {name: local_file(root, name).read_bytes() for name in names}
    atomic_bytes(
        root / "artifact-manifest.json",
        json_bytes(
            {
                "definition": "presentation-seal.v1",
                "files": {name: hashlib.sha256(raw).hexdigest() for name, raw in blobs.items()},
            }
        ),
    )


def read_sealed(root, names):
    saved = read_json(local_file(root, "artifact-manifest.json"))
    if (
        type(saved) is not dict
        or set(saved) != {"definition", "files"}
        or saved["definition"] != "presentation-seal.v1"
        or type(saved["files"]) is not dict
        or set(saved["files"]) != set(names)
    ):
        raise EvidenceError("presentation_seal_invalid")
    blobs = {name: local_file(root, name).read_bytes() for name in names}
    if saved["files"] != {name: hashlib.sha256(raw).hexdigest() for name, raw in blobs.items()}:
        raise EvidenceError("presentation_bytes_changed")
    return blobs
