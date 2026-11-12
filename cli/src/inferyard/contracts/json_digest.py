"""Stable SHA-256 over canonical JSON for frozen plans and asset manifests."""

import hashlib

from inferyard.contracts.validation import json_bytes


def digest(value):
    return hashlib.sha256(json_bytes(value)).hexdigest()
