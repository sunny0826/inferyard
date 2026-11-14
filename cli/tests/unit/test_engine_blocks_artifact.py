"""Retired engine-comparison layouts are rejected, never recomputed or rendered."""

import hashlib
import json

import pytest

from inferyard.application.types import CommandRequest
from inferyard.application.verification import execute as verify
from inferyard.contracts.validation import json_bytes
from inferyard.errors import EvidenceError, UnsupportedFormat


def _artifact(root, saved, *, html=True, seal=True):
    root.mkdir()
    raw = json_bytes(saved)
    (root / "engine-comparison.json").write_bytes(raw)
    names = ["engine-comparison.json"]
    if html:
        (root / "report.html").write_bytes(b"retired presentation")
        names.append("report.html")
    if seal:
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
    return root


def legacy_v1():
    return {
        "definition": "engine-comparison.v1",
        "source_runs": [{"run_id": "a", "path": "runs/a"}],
        "producer": {
            "tool_source_sha256": "a" * 64,
            "template_sha256": "b" * 64,
        },
    }


def legacy_v2():
    return {
        "definition": "engine-comparison.v2",
        "plan": {"path": "plan.json", "sha256": "c" * 64},
        "batch": {"path": "../batch", "plan_sha256": "c" * 64},
        "comparisons": {},
        "source_runs": [{"run_id": "a", "path": "runs/a"}],
        "producer": {
            "tool_source_sha256": "a" * 64,
            "template_sha256": "b" * 64,
        },
    }


def current_document(**overrides):
    value = {
        "schema_version": 3,
        "definition": "engine-comparison.v2",
        "format_version": 2,
        "plan": {"path": "plan.json", "sha256": "c" * 64},
        "batch": {"path": "../batch", "plan_sha256": "c" * 64},
        "comparisons": {},
        "source_runs": [{"run_id": "a", "path": "runs/a"}],
        "producer": {"tool_source_sha256": "a" * 64},
    }
    value.update(overrides)
    return value


@pytest.mark.parametrize("saved", [legacy_v1(), legacy_v2()])
@pytest.mark.parametrize("seal", [True, False])
def test_complete_retired_layout_is_unsupported(tmp_path, saved, seal):
    out = _artifact(tmp_path / "compare", saved, seal=seal)
    with pytest.raises(UnsupportedFormat):
        verify(CommandRequest("verify", run=out))


@pytest.mark.parametrize("saved", [legacy_v1(), legacy_v2()])
def test_bad_seal_precedes_version_rejection(tmp_path, saved):
    out = _artifact(tmp_path / "compare", saved)
    with (out / "report.html").open("ab") as stream:
        stream.write(b"tampered")
    with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
        verify(CommandRequest("verify", run=out))


def test_sealed_retired_layout_requires_its_old_members(tmp_path):
    out = _artifact(tmp_path / "compare", legacy_v1())
    (out / "report.html").unlink()
    with pytest.raises(EvidenceError):
        verify(CommandRequest("verify", run=out))


@pytest.mark.parametrize(
    "saved",
    [
        {**legacy_v1(), "producer": {"tool_source_sha256": "a" * 64}},
        {**legacy_v2(), "source_runs": []},
        {**legacy_v2(), "comparisons": []},
    ],
)
def test_damaged_retired_metadata_is_an_integrity_error(tmp_path, saved):
    out = _artifact(tmp_path / "compare", saved, seal=False)
    with pytest.raises(EvidenceError, match="engine_comparison_format_invalid"):
        verify(CommandRequest("verify", run=out))


def test_current_layout_rejects_template_producer_and_extra_files(tmp_path):
    out = _artifact(
        tmp_path / "template-producer",
        current_document(
            producer={"tool_source_sha256": "a" * 64, "template_sha256": "b" * 64}
        ),
        html=False,
        seal=False,
    )
    with pytest.raises(EvidenceError, match="engine_comparison_format_invalid"):
        verify(CommandRequest("verify", run=out))
    extra = _artifact(
        tmp_path / "extra",
        current_document(),
        html=True,
        seal=False,
    )
    with pytest.raises(EvidenceError, match="engine_comparison_format_invalid"):
        verify(CommandRequest("verify", run=extra))


def test_current_layout_rejects_retired_layout_version(tmp_path):
    out = _artifact(
        tmp_path / "compare", current_document(format_version=1), seal=False
    )
    with pytest.raises(UnsupportedFormat):
        verify(CommandRequest("verify", run=out))


def test_current_layout_requires_integer_layout_version(tmp_path):
    out = _artifact(
        tmp_path / "compare", current_document(format_version="2"), seal=False
    )
    with pytest.raises(EvidenceError, match="invalid_artifact_version"):
        verify(CommandRequest("verify", run=out))


def test_current_layout_rejects_broken_json(tmp_path):
    out = tmp_path / "compare"
    out.mkdir()
    (out / "engine-comparison.json").write_bytes(b"{broken")
    with pytest.raises(EvidenceError):
        verify(CommandRequest("verify", run=out))
