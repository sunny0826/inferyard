"""Reports remain verifiable after their source tree moves."""

import json

from inferyard.application.types import VerificationOptions
from inferyard.reporting import report, sealed_report
from tests.helpers import fixture_run


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
