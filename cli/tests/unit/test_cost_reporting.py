"""Sealed reports must reject unsafe paths and verify the consumed HTML bytes."""

import pytest

from inferyard.application.types import VerificationOptions
from inferyard.errors import EvidenceError
from inferyard.evidence.artifact_seal import read_sealed, seal
from inferyard.reporting import report
from tests.helpers import fixture_run, symlink_or_skip


def test_default_retains_verified_bytes_and_rejects_escaping_paths(tmp_path):
    (tmp_path / "report.html").write_bytes(b"original")
    seal(tmp_path, ["report.html"])
    assert read_sealed(tmp_path, ["report.html"]) == {"report.html": b"original"}
    with pytest.raises(EvidenceError, match="unsafe_evidence_path"):
        seal(tmp_path, ["../outside"])
    outside = tmp_path.parent / (tmp_path.name + "-outside")
    outside.write_bytes(b"outside")
    symlink_or_skip(tmp_path / "escape", outside)
    with pytest.raises(EvidenceError, match="unsafe_evidence_symlink"):
        seal(tmp_path, ["escape"])


def test_multi_run_report_rejects_duplicate_sources(tmp_path):
    roots = [fixture_run(tmp_path / str(i)) for i in range(3)]
    out = tmp_path / "report"
    index = report.write_report(roots, out)
    assert len(index["runs"]) == 3 and index["comparison"] is None
    assert report.verify_report(out)["verified"]
    with pytest.raises(EvidenceError, match="duplicate_report_run"):
        report.build_index([roots[0], roots[1], roots[0]], tmp_path / "duplicate")


def test_rerender_consumes_the_verified_html_snapshot(tmp_path, monkeypatch):
    from inferyard.reporting import sealed_report

    root = fixture_run(tmp_path / "source")
    out = tmp_path / "report"
    report.write_report([root], out)
    original = sealed_report.read_sealed

    def verified_then_changed(*args, **kwargs):
        blobs = original(*args, **kwargs)
        (out / "report.html").write_text("changed after byte verification")
        return blobs

    monkeypatch.setattr(sealed_report, "read_sealed", verified_then_changed)
    assert report.verify_report(out, options=VerificationOptions(rerender=True))["render_checked"]
    with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
        report.verify_report(out)
