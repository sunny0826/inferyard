"""Current report writers can aggregate beyond individual input size limits."""

import pytest

from inferyard.application.types import VerificationOptions
from inferyard.config.loader import MAX_DOCUMENT_BYTES
from inferyard.evidence.storage import EvidenceError, read_json, sha256_file
from inferyard.evidence.trial_reads import TrialReads
from inferyard.reporting.report import verify_report, write_report
from tests.helpers import fixture_run


def test_large_current_report_verifies_and_explicitly_rerenders(tmp_path):
    # Real journal/report writers, synthetic evidence only; no model service.
    source = fixture_run(
        tmp_path / "source", states=["completed"], model="synthetic-" + "m" * (6 * 1024**2)
    )
    assert max(p.stat().st_size for p in source.glob("*.json")) < MAX_DOCUMENT_BYTES
    before = {p.name: sha256_file(p) for p in source.iterdir() if p.is_file()}
    out = tmp_path / "report"
    index = write_report([source], out)
    assert index["report_format_version"] == 7
    path = out / "index.json"
    assert path.stat().st_size > 16 * 1024**2
    saved = {p.name: sha256_file(p) for p in out.iterdir() if p.is_file()}

    assert read_json(path) == index
    reads = TrialReads(out)
    assert reads.json("index.json") == index
    assert reads.observed()["index.json"] == (path.stat().st_size, saved["index.json"])
    assert not reads.errors
    for rerender in (False, True):
        result = verify_report(out, options=VerificationOptions(rerender=rerender))
        assert result == {
            "verified": True,
            "source_runs": 1,
            "bytes_verified": True,
            "sources_verified": True,
            "semantic_verified": True,
            "render_checked": rerender,
        }
        assert saved == {p.name: sha256_file(p) for p in out.iterdir() if p.is_file()}
        assert before == {p.name: sha256_file(p) for p in source.iterdir() if p.is_file()}


@pytest.mark.parametrize(
    "raw", [b'{"x":"\xff"}', b'{"x":1,"x":2}', b'{"x":NaN}', b'{"x":Infinity}']
)
def test_generic_readers_retain_strict_json_validation(tmp_path, raw):
    path = tmp_path / "input.json"
    path.write_bytes(raw)
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        read_json(path)
    reads = TrialReads(tmp_path)
    assert reads.json(path.name) is None
    assert str(reads.errors[path.name]) == "invalid_json_evidence"
    assert path.read_bytes() == raw
