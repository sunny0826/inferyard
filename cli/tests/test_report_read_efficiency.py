"""Studio reports remain readable after their source tree moves."""

import json

from inferyard.reporting.studio_report import read_studio_report, write_studio_report
from tests.helpers import fixture_run


def test_noncanonical_studio_report_reads_without_revisiting_sources(tmp_path):
    base = tmp_path / "original"
    root = fixture_run(base / "source")
    out = base / "report"
    saved = write_studio_report([root], out)
    raw = json.dumps(dict(reversed(list(saved.items()))), ensure_ascii=False, indent=3).encode()
    (out / "studio.json").write_bytes(raw)
    moved = tmp_path / "moved"
    base.rename(moved)
    assert read_studio_report(moved / "report") == saved
    assert (moved / "report/studio.json").read_bytes() == raw
