"""Stable version-derived tool identity (no source-tree byte hashing)."""

import hashlib

from inferyard import __version__


def tool_source_hash(*, file_hashes=None):
    del file_hashes
    return hashlib.sha256(f"inferyard:{__version__}".encode("utf-8")).hexdigest()
