"""The reviewable corpus is reproducible and cannot reuse its parents' approval."""

import json
from collections import Counter
from pathlib import Path

import pytest

from inferyard.analysis.scoring import score_case
from inferyard.config.bundle import require_review
from inferyard.contracts.validation import ContractError, validate_document
from scripts.build_phase2_bundle import build, render

ROOT = Path(__file__).resolve().parents[2]
BUNDLE = json.loads((ROOT / "bundles/zh-core.json").read_text())
BOUNDARIES = json.loads((ROOT / "bundles/zh-core-v2.boundaries.json").read_text())


def test_exact_composition_and_reproducible_review_artifacts():
    expected, boundaries = build()
    actual = {k: v for k, v in BUNDLE.items() if k != "review_provenance"}
    actual["review_records"] = []
    assert actual == expected
    assert BOUNDARIES == boundaries
    source = json.loads(BUNDLE["review_provenance"][0]["source_json"])
    assert (ROOT / "bundles/zh-core-v2-review.html").read_text() == render(source, boundaries)
    original = json.loads((ROOT / "bundles/zh-smoke.json").read_text())
    assert BUNDLE["cases"][:40] == original["cases"]
    inherited_source = json.loads(original["review_provenance"][0]["source_json"])
    assert source["inherited_bundles"] == [inherited_source]
    assert "inherited_bundles" not in BUNDLE
    assert Counter(c["category"] for c in BUNDLE["cases"]) == dict.fromkeys(
        ["instruction", "extraction", "qa", "math", "classification", "structured"], 20
    )
    assert Counter(
        c["rules"]["expected"] for c in BUNDLE["cases"] if c["category"] == "classification"
    ) == dict.fromkeys(["物流", "账务", "技术", "其他"], 5)
    validate_document("bundle", BUNDLE)


@pytest.mark.parametrize("task", BUNDLE["cases"][40:], ids=lambda c: c["case_id"])
def test_each_new_reference_and_both_boundaries(task):
    assert (
        score_case(task, task["reference_answer"], BUNDLE["answer_policy"])["quality_state"]
        == "pass"
    )
    examples = BOUNDARIES[task["case_id"]]
    assert {e["expected_state"] for e in examples} == {"pass", "fail"}
    for example in examples:
        result = score_case(task, example["answer"], BUNDLE["answer_policy"])
        assert result["quality_state"] == example["expected_state"]


def test_no_parent_approval_can_authorize_new_content():
    unreviewed = {**BUNDLE, "review_records": []}
    with pytest.raises(ContractError, match="original review record was removed"):
        require_review(unreviewed)
    unreviewed.pop("review_provenance")
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(unreviewed)


def test_review_html_escapes_task_content():
    synthetic = json.loads(json.dumps(BUNDLE))
    synthetic["cases"][40]["prompt"] = "<script>window.attack=true</script>"
    page = render(synthetic, BOUNDARIES)
    assert "<script>window.attack" not in page
    assert "&lt;script&gt;window.attack" in page
