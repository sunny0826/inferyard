"""Private read-only checks for retired presentation layouts.

Only version identification for old reports/comparisons uses this module: it
validates the exact old seal and retains the JSON bytes needed to name a saved
version. It never renders, writes, migrates or returns a verified artifact.
"""

import hashlib
from pathlib import Path

from inferyard.errors import EvidenceError
from inferyard.evidence.storage import local_file, read_json, sha256_file

DEFINITION = "presentation-seal.v1"
SEAL = "artifact-manifest.json"


def sealed_members(root: Path) -> tuple[str, ...]:
    names = ["index.json", "report.html"]
    if local_file(root, "comparison.json").exists():
        names.append("comparison.json")
    return tuple(names)


def _check_seal(root: Path, names: tuple[str, ...]) -> dict[str, bytes]:
    saved = read_json(local_file(root, SEAL))
    if (
        type(saved) is not dict
        or set(saved) != {"definition", "files"}
        or saved["definition"] != DEFINITION
        or type(saved["files"]) is not dict
        or set(saved["files"]) != set(names)
    ):
        raise EvidenceError("presentation_seal_invalid")
    hashes, blobs = {}, {}
    for name in names:
        path = local_file(root, name)
        if name == "index.json":
            raw = path.read_bytes()
            blobs[name] = raw
            hashes[name] = hashlib.sha256(raw).hexdigest()
        else:
            hashes[name] = sha256_file(path)
    if saved["files"] != hashes:
        raise EvidenceError("presentation_bytes_changed")
    return blobs


def legacy_index_bytes(root: Path) -> bytes | None:
    """Validate an old seal when present and retain only the saved index bytes."""
    if not local_file(root, SEAL).exists():
        return None
    return _check_seal(root, sealed_members(root))["index.json"]
