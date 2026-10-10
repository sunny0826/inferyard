"""Report copy and read optimizations preserve normalization and verified snapshots."""

import json
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.application.types import VerificationOptions
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json
from inferyard.reporting import report, sealed_report
from tests.helpers import fixture_run


def test_portable_paths_leave_source_and_large_run_payloads_untouched(tmp_path):
    summary = {"requests": [{"answer": "x" * 10_000}]}
    source = {"path": str(tmp_path / "runs" / "a"), "run_id": "a"}
    comparison_path = tmp_path / "comparison"
    index = {
        "runs": [{"source": source, "summary": summary}],
        "contents": {"a": "x" * 10_000},
        "comparison_source": {"path": str(comparison_path), "sha256": "a" * 64},
        "comparison": {
            "format_version": 4,
            "source_runs": [{"path": "../runs/a"}],
            "performance_evidence": [{"source": {"path": "../runs/a"}}],
            "nested": [[1]],
        },
    }
    original = deepcopy(index)
    portable = sealed_report.portable_index(index, tmp_path / "report", comparison_path)
    assert index == original
    assert portable["runs"][0]["source"]["path"] == "../runs/a"
    assert portable["comparison_source"]["path"] == "../comparison"
    assert portable["comparison"]["source_runs"][0]["path"] == "../runs/a"
    assert portable["comparison"]["performance_evidence"][0]["source"]["path"] == "../runs/a"
    assert portable["runs"][0]["summary"] is summary
    assert portable["contents"] is index["contents"]
    portable["comparison"]["nested"][0].append(2)
    portable["runs"][0]["source"]["path"] = "changed"
    portable["comparison_source"]["path"] = "changed"
    assert index == original


def test_written_index_is_detached_and_json_normalized(tmp_path, monkeypatch):
    root = fixture_run(tmp_path / "source")
    original = report.build_index
    built = []

    def with_python_values(*args, **kwargs):
        index = original(*args, **kwargs)
        index["normalization"] = {1: ("a", {2: ("b",)})}
        built.append(index)
        return index

    monkeypatch.setattr(report, "build_index", with_python_values)
    out = tmp_path / "report"
    saved = report.write_report([root], out)
    assert saved["normalization"] == {"1": ["a", {"2": ["b"]}]}
    assert saved == read_json(out / "index.json")
    before = deepcopy(built[0])
    saved["runs"][0]["summary"]["requests_total"] = "changed"
    saved["contents"]["new"] = "changed"
    saved["normalization"]["1"].append("changed")
    assert built[0] == before


def test_stable_index_is_parsed_once_and_verified_bytes_are_released(tmp_path, monkeypatch):
    root = fixture_run(tmp_path / "source")
    out = tmp_path / "report"
    report.write_report([root], out)
    originals = {path: path.read_bytes() for path in root.iterdir() if path.is_file()}
    original = sealed_report.read_sealed
    snapshots = []

    def retained(*args, **kwargs):
        blobs = original(*args, **kwargs)
        snapshots.append(blobs)
        return blobs

    def duplicate_parse(_raw):
        pytest.fail("unchanged verified index must reuse its first strict parse")

    monkeypatch.setattr(sealed_report, "read_sealed", retained)
    monkeypatch.setattr(sealed_report, "strict_json_loads", duplicate_parse)
    assert report.verify_report(out)["sources_verified"]
    assert report.verify_report(out, options=VerificationOptions(rerender=True))["render_checked"]
    assert all("index.json" not in blobs for blobs in snapshots)
    assert "report.html" not in snapshots[0]
    assert "report.html" in snapshots[1]
    assert all(path.read_bytes() == raw for path, raw in originals.items())
    (root / "events.jsonl").write_bytes(b"{}\n")
    with pytest.raises(EvidenceError):
        report.verify_report(out)


def test_noncanonical_index_remains_valid_after_moving_sources(tmp_path):
    base = tmp_path / "original"
    root = fixture_run(base / "source")
    out = base / "report"
    saved = report.write_report([root], out)
    raw = json.dumps(dict(reversed(list(saved.items()))), ensure_ascii=False, indent=3).encode()
    (out / "index.json").write_bytes(raw)
    (out / "artifact-manifest.json").unlink()
    sealed_report.seal_report(out)
    moved = tmp_path / "moved"
    base.rename(moved)
    assert report.verify_report(moved / "report")["sources_verified"]
    assert report.verify_report(moved / "report", options=VerificationOptions(rerender=True))[
        "render_checked"
    ]
    assert (moved / "report" / "index.json").read_bytes() == raw


@pytest.mark.parametrize("change", ["equivalent", "semantic", "duplicate"])
def test_second_read_changes_keep_existing_semantic_policy(tmp_path, monkeypatch, change):
    root = fixture_run(tmp_path / "source")
    out = tmp_path / "report"
    saved = report.write_report([root], out)
    original = sealed_report.read_sealed
    parser = sealed_report.strict_json_loads
    parsed = []

    def changed_then_verified(*args, **kwargs):
        if change == "semantic":
            saved["contents"]["new"] = "changed"
        raw = json.dumps(dict(reversed(list(saved.items()))), indent=2)
        if change == "duplicate":
            raw = '{"schema_version":3,' + raw[1:]
        (out / "index.json").write_text(raw)
        (out / "artifact-manifest.json").unlink()
        sealed_report.seal_report(out)
        return original(*args, **kwargs)

    def fallback(raw):
        parsed.append(raw)
        return parser(raw)

    monkeypatch.setattr(sealed_report, "read_sealed", changed_then_verified)
    monkeypatch.setattr(sealed_report, "strict_json_loads", fallback)
    if change == "equivalent":
        assert report.verify_report(out)["verified"]
    elif change == "semantic":
        with pytest.raises(EvidenceError, match="report_changed_during_read"):
            report.verify_report(out)
    else:
        with pytest.raises(ContractError):
            report.verify_report(out)
    assert len(parsed) == 1


@pytest.mark.parametrize("raw", [b"{", b'{"a":1,"a":2}', b'{"a":NaN}', b"\xff"])
def test_first_index_parse_preserves_invalid_json_mapping(tmp_path, raw):
    tmp_path.joinpath("index.json").write_bytes(raw)
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        report.verify_report(tmp_path)


def test_first_index_read_preserves_io_error_mapping(tmp_path, monkeypatch):
    tmp_path.joinpath("index.json").write_bytes(json_bytes({}))
    original = Path.open

    def failed(path, *args, **kwargs):
        if path.name == "index.json":
            raise OSError("unavailable")
        return original(path, *args, **kwargs)

    monkeypatch.setattr(Path, "open", failed)
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        report.verify_report(tmp_path)
