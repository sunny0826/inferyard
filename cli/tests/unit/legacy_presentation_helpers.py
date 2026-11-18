"""Retired presentation layouts are identified without rendering or verifying."""

import hashlib

import pytest

from inferyard.contracts.validation import json_bytes
from inferyard.errors import EvidenceError
from inferyard.evidence.legacy_presentation_seal import legacy_index_bytes
from tests.helpers import symlink_or_skip


def legacy_layout(root, *, version=8, seal=True, html=True, comparison=False):
    root.mkdir(parents=True, exist_ok=True)
    index = json_bytes({"schema_version": 3, "report_format_version": version})
    (root / "index.json").write_bytes(index)
    if html:
        (root / "report.html").write_text("retired presentation")
    if comparison:
        (root / "comparison.json").write_bytes(
            json_bytes({"schema_version": 3, "format_version": 4})
        )
    if seal:
        names = ["index.json", "report.html"]
        if comparison:
            names.append("comparison.json")
        (root / "artifact-manifest.json").write_bytes(
            json_bytes(
                {
                    "definition": "presentation-seal.v1",
                    "files": {
                        name: hashlib.sha256((root / name).read_bytes()).hexdigest()
                        for name in names
                    },
                }
            )
        )
    return index
