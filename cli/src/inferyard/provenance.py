"""Content identity of the installed measurement implementation and templates."""

import hashlib
from pathlib import Path


def tool_source_hash(*, file_hashes=None):
    root = Path(__file__).parent
    digest = hashlib.sha256()
    if file_hashes is None:
        file_hashes = {
            path.relative_to(root).as_posix(): hashlib.sha256(path.read_bytes()).hexdigest()
            for path in root.rglob("*")
            if path.suffix in (".py", ".html", ".json")
        }
    for name, value in sorted(file_hashes.items()):
        digest.update(name.encode() + b"\0")
        digest.update(bytes.fromhex(value))
    return digest.hexdigest()
