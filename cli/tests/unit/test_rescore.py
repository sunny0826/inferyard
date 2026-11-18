from copy import deepcopy

from inferyard.reporting.rescore import rescore_rows
from tests.unit.test_scoring_contract import BUNDLE, POLICY


def rows():
    case = BUNDLE["cases"][0]
    return case, [
        {
            "case_id": case["case_id"],
            "category": case["category"],
            "request_id": "request-1",
            "execution_state": "completed",
            "content": case["reference_answer"],
            "score": None,
        }
    ]


def test_rescoring_changes_only_scores_and_keeps_failed_attempts():
    case, source = rows()
    source.extend(
        {**source[0], "request_id": state, "execution_state": state}
        for state in ("failed", "invalid", "cancelled", "not_executed")
    )
    before = deepcopy(source)
    updated, changes = rescore_rows([case], source, POLICY)
    assert source == before
    assert updated[0]["score"]["quality_state"] == "pass"
    assert all(row["score"] is None for row in updated[1:])
    assert [r["execution_state"] for r in updated] == [r["execution_state"] for r in before]
    assert len(changes) == len(source)
