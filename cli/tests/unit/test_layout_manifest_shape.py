"""Unsupported declarations still require genuine physical byte counts."""

import pytest

from inferyard.cli import main
from inferyard.evidence.storage import json_bytes, read_json, sha256_file
from tests.evidence_layout_helpers import build_current_original, pack_layout2, seal_layout


@pytest.mark.parametrize("byte_count, expected", [(0, 2), (False, 4), (0.0, 4), ("0", 4), (-1, 4)])
def test_unknown_layout_checks_physical_byte_count_type(tmp_path, byte_count, expected):
    original = build_current_original(tmp_path / "original")
    root = pack_layout2(original, tmp_path / "container")
    inputs = read_json(root / "inputs.json")
    inputs["evidence_layout_version"] = 99
    (root / "inputs.json").write_bytes(json_bytes(inputs))
    (root / "empty.fixture").write_bytes(b"")
    seal_layout(root, inputs["documents"]["run"]["payload"])
    manifest = read_json(root / "manifest.json")
    manifest["evidence_layout_version"] = 99
    manifest["files"]["empty.fixture"]["bytes"] = byte_count
    (root / "manifest.json").write_bytes(json_bytes(manifest))
    before = {p.name: sha256_file(p) for p in root.iterdir()}

    assert main(["verify", "--path", str(root)]) == expected
    assert {p.name: sha256_file(p) for p in root.iterdir()} == before
