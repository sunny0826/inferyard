from pathlib import Path

import pytest

from inferyard.analysis.scoring import score_case as original
from inferyard.analysis.scoring_revision import (
    VERSION,
    resolve,
    score_case,
    scorer_hash,
    unscorable,
)
from inferyard.evidence.storage import read_json
from tests.unit.test_scoring_contract import POLICY


@pytest.mark.parametrize("answer", ["2", "3", "two", "2kg", "1e99999999999999"])
def test_numeric_diagnostics_preserve_original_verdict(answer):
    case = next(
        c for c in read_json(Path("bundles/zh-core.json"))["cases"] if c["category"] == "math"
    )
    before = original(case, answer, POLICY)
    after = score_case(case, answer, POLICY)
    for key in ("quality_state", "content_ok", "format_ok"):
        assert after[key] == before[key]
    assert [r["rule"] for r in after["rule_results"]] == [
        "numeric_unit_format",
        "numeric_tolerance_match",
    ]
    assert after["scorer_version"] == VERSION
    assert after["scorer_sha256"] != before["scorer_sha256"]
    assert after["scorer_sha256"] == scorer_hash()


def test_missing_scores_use_selected_revision_identity():
    score = unscorable("math", "redacted_scoring_input")
    assert score["quality_state"] == "unscorable"
    assert score["scorer_version"] == VERSION
    assert resolve(VERSION)[2]("math", "redacted_scoring_input") == score
