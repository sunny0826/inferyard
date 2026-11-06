"""Command reuse must still reject changed evidence and scorer identities."""

import pytest

from inferyard.analysis import scoring
from inferyard.application.types import CommandRequest
from inferyard.evidence.storage import EvidenceError
from inferyard.reporting import comparison_report, rescore
from inferyard.reporting.report import verify_report
from tests.helpers import fixture_run
from tests.unit.test_scoring_contract import BUNDLE, POLICY


def test_recursive_rescore_uses_one_identity_per_version_and_one_reduction(tmp_path, monkeypatch):
    root = fixture_run(tmp_path / "source")
    first, second = tmp_path / "first", tmp_path / "second"
    rescore.write_rescore(root, first, scorer_id="phase2.v1", reason="first")
    rescore.write_rescore(
        root, second, scorer_id="phase2.v2", reason="second", parent_path=first / "analysis.json"
    )
    assert rescore.verify_rescore(second)["verified"]
    monkeypatch.setattr(scoring, "scorer_hash", lambda: "f" * 64)
    with pytest.raises(EvidenceError, match="rescore_scorer_identity_changed"):
        rescore.verify_rescore(second)
    with (root / "events.jsonl").open("ab") as stream:
        stream.write(b"\n")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        rescore.verify_rescore(second)


def test_compare_reduces_each_source_once_and_compares_once(tmp_path):
    roots = [fixture_run(tmp_path / side) for side in ("left", "right")]
    out = tmp_path / "comparison"
    code, _ = comparison_report.execute(
        CommandRequest("compare", left=roots[0], right=roots[1], out=out)
    )
    assert code == 0
    assert verify_report(out)["verified"]
    (roots[0] / "memory.jsonl").write_text("changed")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        comparison_report.verify_comparison(out)


@pytest.mark.parametrize("damage", ["shape", "identity", "category"])
def test_custom_scorer_result_still_checked_and_does_not_leak(damage):
    case = BUNDLE["cases"][0]
    source = [
        {
            "case_id": case["case_id"],
            "category": case["category"],
            "request_id": "x",
            "execution_state": "completed",
            "content": case["reference_answer"],
            "score": None,
        }
    ]

    def custom(*_):
        result = scoring.score_case(case, case["reference_answer"], POLICY)
        if damage == "shape":
            result["private-response"] = "secret"
        elif damage == "identity":
            result["scorer_sha256"] = "0" * 64
        else:
            result["category"] = "math"
        return result

    if damage in ("identity", "category"):
        with pytest.raises(EvidenceError, match="rescore_scorer_identity_mismatch"):
            rescore.rescore_rows([case], source, POLICY, scorer=custom)
        return
    rows, _ = rescore.rescore_rows([case], source, POLICY, scorer=custom)
    assert rows[0]["score"]["reason"] == "scorer_exception"
    assert "secret" not in str(rows)


def test_explicit_custom_identity_is_not_bypassed_by_builtin_scorer():
    case = BUNDLE["cases"][0]
    row = {
        "case_id": case["case_id"],
        "category": case["category"],
        "request_id": "x",
        "execution_state": "completed",
        "content": case["reference_answer"],
        "score": None,
    }
    with pytest.raises(EvidenceError, match="rescore_scorer_identity_mismatch"):
        rescore.rescore_rows([case], [row, row], POLICY, identity=lambda: "0" * 64)
