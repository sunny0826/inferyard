"""Current Studio publication rejects duplicate sources and never links comparisons."""

import pytest

from inferyard.errors import EvidenceError
from inferyard.reporting.studio_report import build_studio_report, write_studio_report
from tests.helpers import fixture_run


def test_multi_run_studio_report_rejects_duplicate_sources(tmp_path):
    roots = [fixture_run(tmp_path / str(i)) for i in range(3)]
    out = tmp_path / "report"
    document = write_studio_report(roots, out)
    assert len(document["runs"]) == 3
    assert {path.name for path in out.iterdir()} == {"studio.json"}
    with pytest.raises(EvidenceError, match="duplicate_report_run"):
        build_studio_report([roots[0], roots[1], roots[0]])
