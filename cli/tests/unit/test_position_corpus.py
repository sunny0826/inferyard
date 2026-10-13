import runpy
from pathlib import Path

import pytest

from inferyard.analysis.position import position_pattern
from inferyard.analysis.scoring import score_case
from inferyard.config.bundle import require_review
from inferyard.contracts.validation import ContractError


def test_generated_position_corpus_preserves_lengths_evidence_and_two_families():
    module = runpy.run_path(str(Path(__file__).parents[2] / "scripts/build_position_bundle.py"))
    bundle, protocol = module["build"]()
    assert (bundle, protocol) == module["build"]()
    assert len(bundle["cases"]) == 18
    assert protocol["independent_families"] == 2
    answers = {c["reference_answer"] for c in bundle["cases"]}
    assert len(answers) == 2
    families = {f["family_id"]: f for f in module["FAMILIES"]}
    for case, record, cohort in zip(
        bundle["cases"], protocol["position_cases"], protocol["cohorts"], strict=True
    ):
        assert record["body_end"] - record["body_start"] == cohort["body_characters"]
        family = families[record["family_id"]]
        assert position_pattern(record)[1] == [cohort["position"]] * len(family["evidence"])
        assert all(case["prompt"].count(evidence) == 1 for evidence in family["evidence"])
        assert "无关条目" not in case["prompt"]
        body = case["prompt"][record["body_start"] : record["body_end"]]
        lines = body.splitlines()
        for evidence in family["evidence"]:
            assert lines.count(evidence) == 1
        assert all(
            not line.strip() or line == "背景记录：石桥旁有树。" or line in family["evidence"]
            for line in lines
        )
        assert (
            score_case(case, case["reference_answer"], bundle["answer_policy"])["quality_state"]
            == "pass"
        )
        for wrong in ("", "不知道", next(a for a in answers if a != case["reference_answer"])):
            assert score_case(case, wrong, bundle["answer_policy"])["quality_state"] == "fail"
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)
