"""Retired engine-fit v2 layouts: complete evidence is unsupported, damage is integrity."""

import hashlib
import json
import shutil

import pytest

from inferyard.contracts.validation import json_bytes
from inferyard.errors import EvidenceError, UnsupportedFormat
from inferyard.reporting.engine_fit import compare, verify
from tests.unit.test_engine_fit_report import _replace, _seal


@pytest.fixture
def plan():
    from tests.unit.test_engine_fit_report import plan as fixture

    return fixture.__wrapped__()


def _retire_manifest(path, definition="engine_fit_manifest.v2"):
    manifest = json.loads((path / "manifest.json").read_text())
    retired = b"retired presentation"
    (path / "report.html").write_bytes(retired)
    manifest["definition"] = definition
    manifest["files"]["report.html"] = hashlib.sha256(retired).hexdigest()
    (path / "manifest.json").write_bytes(json_bytes(manifest))
    return path


def _retired_comparison(tmp_path, plan):
    """Build a genuine v2 comparison: v2 top-level and v2 embedded source runs."""
    tmp_path.mkdir(parents=True, exist_ok=True)
    left = _seal(tmp_path / "left", plan)
    right = _seal(tmp_path / "right", plan, "sglang")
    out = tmp_path / "comparison"
    compare([left, right], out)
    for index in ("0", "1"):
        _retire_manifest(out / "sources" / index)
    saved = json.loads((out / "comparison.json").read_text())
    for index, source in enumerate(saved["source_runs"]):
        source["manifest_sha256"] = hashlib.sha256(
            (out / "sources" / str(index) / "manifest.json").read_bytes()
        ).hexdigest()
    _replace(out, "comparison.json", saved)
    return _retire_manifest(out)


@pytest.mark.parametrize("definition", ["engine_fit_manifest.v1", "engine_fit_manifest.v2"])
def test_complete_retired_run_manifest_is_unsupported(tmp_path, plan, definition):
    path = _retire_manifest(_seal(tmp_path / "run", plan), definition)
    with pytest.raises(UnsupportedFormat):
        verify(path)


def test_complete_retired_comparison_manifest_is_unsupported(tmp_path, plan):
    path = _retired_comparison(tmp_path, plan)
    for index in ("0", "1"):
        manifest = json.loads((path / "sources" / index / "manifest.json").read_text())
        assert manifest["definition"] == "engine_fit_manifest.v2"
        assert (path / "sources" / index / "report.html").exists()
    with pytest.raises(UnsupportedFormat):
        verify(path)


def test_retired_comparison_with_missing_or_changed_sources_is_integrity_error(tmp_path, plan):
    missing = _retired_comparison(tmp_path / "missing", plan)
    shutil.rmtree(missing / "sources" / "0")
    with pytest.raises(EvidenceError):
        verify(missing)
    changed = _retired_comparison(tmp_path / "changed", plan)
    (changed / "sources" / "0" / "run.json").write_bytes(b"{}")
    with pytest.raises(EvidenceError):
        verify(changed)


def test_retired_run_with_self_consistent_top_hash_but_bad_members_is_integrity_error(
    tmp_path, plan
):
    broken_json = _retire_manifest(_seal(tmp_path / "json", plan))
    _replace(broken_json, "run.json", b"{broken")
    with pytest.raises(EvidenceError, match="invalid_json"):
        verify(broken_json)
    bad_semantics = _retire_manifest(_seal(tmp_path / "semantics", plan))
    run = json.loads((bad_semantics / "run.json").read_text())
    run["counts"]["completed"] = 99
    _replace(bad_semantics, "run.json", run)
    with pytest.raises(EvidenceError):
        verify(bad_semantics)


@pytest.mark.parametrize(
    "mutate",
    [
        lambda manifest: manifest.update(definition=2),
        lambda manifest: manifest.update(schema_version="3"),
    ],
)
def test_retired_version_field_type_errors_are_integrity_errors(tmp_path, plan, mutate):
    path = _retire_manifest(_seal(tmp_path / "run", plan))
    manifest = json.loads((path / "manifest.json").read_text())
    mutate(manifest)
    (path / "manifest.json").write_bytes(json_bytes(manifest))
    with pytest.raises(EvidenceError):
        verify(path)


def test_compare_rejects_complete_retired_sources_but_reports_damage(tmp_path, plan):
    left = _retire_manifest(_seal(tmp_path / "left", plan))
    right = _retire_manifest(_seal(tmp_path / "right", plan, "sglang"))
    with pytest.raises(UnsupportedFormat):
        compare([left, right], tmp_path / "retired")
    assert not (tmp_path / "retired").exists()
    _replace(right, "run.json", b"{broken")
    with pytest.raises(EvidenceError):
        compare([right, left], tmp_path / "damaged")


def test_damaged_retired_manifest_is_an_integrity_error(tmp_path, plan):
    missing = _retire_manifest(_seal(tmp_path / "run", plan))
    (missing / "report.html").unlink()
    with pytest.raises(EvidenceError, match="missing_file"):
        verify(missing)
    changed = _retire_manifest(_seal(tmp_path / "other", plan))
    (changed / "report.html").write_bytes(b"changed bytes")
    with pytest.raises(EvidenceError, match="hash_mismatch"):
        verify(changed)
