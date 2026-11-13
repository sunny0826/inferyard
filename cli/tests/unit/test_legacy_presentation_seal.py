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


def test_exact_old_seal_retains_only_index_bytes_and_never_verifies(tmp_path):
    index = legacy_layout(tmp_path)
    assert legacy_index_bytes(tmp_path) == index
    assert legacy_index_bytes(tmp_path).startswith(b"{")


def test_layout_without_seal_is_identified_by_bytes_alone(tmp_path):
    index = legacy_layout(tmp_path, seal=False)
    assert legacy_index_bytes(tmp_path) is None
    assert (tmp_path / "index.json").read_bytes() == index


@pytest.mark.parametrize("member", ["index.json", "report.html"])
def test_changed_member_byte_is_rejected_before_version_decisions(tmp_path, member):
    legacy_layout(tmp_path)
    with (tmp_path / member).open("ab") as stream:
        stream.write(b"tampered")
    with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
        legacy_index_bytes(tmp_path)


def test_missing_member_or_manifest_shape_is_rejected(tmp_path):
    legacy_layout(tmp_path)
    (tmp_path / "report.html").unlink()
    with pytest.raises((EvidenceError, OSError)):
        legacy_index_bytes(tmp_path)
    legacy_layout(tmp_path / "second")
    manifest = (tmp_path / "second/artifact-manifest.json").read_bytes()
    (tmp_path / "second/artifact-manifest.json").write_bytes(manifest[:-1] + b",}")
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        legacy_index_bytes(tmp_path / "second")


def test_seal_members_must_match_the_declared_directories_shape(tmp_path):
    legacy_layout(tmp_path)
    sealed = (tmp_path / "artifact-manifest.json").read_bytes()
    (tmp_path / "comparison.json").write_bytes(
        json_bytes({"schema_version": 3, "format_version": 4})
    )
    with pytest.raises(EvidenceError, match="presentation_seal_invalid"):
        legacy_index_bytes(tmp_path)
    (tmp_path / "artifact-manifest.json").write_bytes(sealed)
    (tmp_path / "comparison.json").unlink()


def test_escaping_member_is_rejected_without_reading_outside(tmp_path):
    legacy_layout(tmp_path)
    outside = tmp_path.parent / (tmp_path.name + "-outside")
    outside.write_bytes(b"outside")
    (tmp_path / "report.html").unlink()
    symlink_or_skip(tmp_path / "report.html", outside)
    with pytest.raises(EvidenceError, match="unsafe_evidence_symlink"):
        legacy_index_bytes(tmp_path)
