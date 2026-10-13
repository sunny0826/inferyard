"""Reports verify all bytes while retaining only consumed snapshots and run data."""

import weakref
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.application.types import VerificationOptions
from inferyard.evidence.artifact_seal import read_sealed, seal
from inferyard.evidence.storage import EvidenceError
from inferyard.reporting import report
from inferyard.reporting.sealed_report import portable_index
from tests.helpers import fixture_run, symlink_or_skip


def test_seal_streams_and_selective_reads_still_verify_html(tmp_path, monkeypatch):
    names = ["index.json", "report.html"]
    for name in names:
        (tmp_path / name).write_bytes(b"x" * 100_000)
    original = Path.read_bytes

    def bounded(path):
        if path.name == "report.html":
            pytest.fail("unconsumed HTML must not be retained")
        return original(path)

    monkeypatch.setattr(Path, "read_bytes", bounded)
    seal(tmp_path, names)
    assert read_sealed(tmp_path, names, retain={"index.json"}) == {"index.json": b"x" * 100_000}
    (tmp_path / "report.html").write_bytes(b"y" * 100_000)
    with pytest.raises(EvidenceError, match="presentation_bytes_changed"):
        read_sealed(tmp_path, names, retain={"index.json"})


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


def test_portable_index_does_not_mutate_nested_input(tmp_path):
    source = {"path": str(tmp_path / "source"), "run_id": "r"}
    index = {"runs": [{"source": source}], "comparison": {"nested": [[1]]}}
    before = deepcopy(index)
    copied = portable_index(index, tmp_path)
    copied["comparison"]["nested"][0].append(2)
    assert index == before
    assert copied["runs"][0]["source"]["path"] == "source"


def test_nonpair_releases_raw_samples_before_reading_next_run(tmp_path, monkeypatch):
    roots = [fixture_run(tmp_path / str(i)) for i in range(3)]
    original = report.comparison_input
    references = []

    class Samples(list):
        pass

    def read(root, **kwargs):
        assert all(ref() is None for ref in references)
        data, source = original(root, **kwargs)
        data["samples"] = Samples(data["samples"])
        references.append(weakref.ref(data["samples"]))
        return data, source

    monkeypatch.setattr(report, "comparison_input", read)
    out = tmp_path / "report"
    index = report.write_report(roots, out)
    assert len(index["runs"]) == 3 and index["comparison"] is None
    assert all(ref() is None for ref in references)
    assert report.verify_report(out)["verified"]
    assert report.verify_report(out, options=VerificationOptions(rerender=True))["render_checked"]
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
