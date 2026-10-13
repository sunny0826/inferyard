"""Prompt repair cannot rewrite old scores or inherit approval for changed content."""

from copy import deepcopy

import pytest

from inferyard.analysis.scoring import score_case
from inferyard.config.bundle import require_review
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import read_json, sha256_file
from scripts.build_structured_clarity_bundle import SOURCE, build, write_outputs


@pytest.fixture
def source():
    return read_json(SOURCE)


def test_reviewed_source_and_all_rules_references_are_preserved(source):
    before = deepcopy(source)
    digest = sha256_file(SOURCE)
    candidate, changes = build(source)
    assert source == before
    assert sha256_file(SOURCE) == digest
    assert len(candidate["cases"]) == 120
    assert [c["case_id"] for c in candidate["cases"]] == [c["case_id"] for c in source["cases"]]
    changed = []
    for original, revised in zip(source["cases"], candidate["cases"], strict=True):
        if original == revised:
            continue
        changed.append(original["case_id"])
        assert revised["category"] == "structured"
        assert {k: v for k, v in original.items() if k != "prompt"} == {
            k: v for k, v in revised.items() if k != "prompt"
        }
        assert revised["prompt"].startswith(original["prompt"].split("输出", 1)[0])
    assert len(changed) == 20
    assert changed == [c["case_id"] for c in changes]
    assert candidate["answer_policy"] == source["answer_policy"]


def test_new_prompts_cannot_inherit_the_old_human_approval(source):
    require_review(source)
    candidate, _ = build(source)
    assert not candidate["review_records"]
    assert "review_provenance" not in candidate
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(candidate)
    candidate["review_records"] = deepcopy(source["review_records"])
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(candidate)


@pytest.mark.parametrize(
    ("case_id", "unwrapped"),
    [
        ("structured-03", '[{"name":"苹果","count":2},{"name":"梨","count":3}]'),
        ("structured-05", '{"title":"周会","attendees":[]}'),
    ],
)
def test_observed_wrapper_failures_remain_failures_without_relaxing_rules(
    source, case_id, unwrapped
):
    candidate, _ = build(source)
    for bundle in (source, candidate):
        case = next(c for c in bundle["cases"] if c["case_id"] == case_id)
        score = score_case(case, unwrapped, bundle["answer_policy"])
        assert score["json_parse_ok"] is True
        assert score["schema_ok"] is False
        assert score["quality_state"] == "fail"
        assert (
            score_case(case, case["reference_answer"], bundle["answer_policy"])["quality_state"]
            == "pass"
        )


def test_generator_rejects_overwrite_before_writing_any_new_file(tmp_path):
    existing = tmp_path / "human-review.md"
    existing.write_text("human-owned")
    fresh = tmp_path / "new.json"
    with pytest.raises(ValueError, match="refusing_to_overwrite"):
        write_outputs({fresh: b"new", existing: b"replaced"})
    assert not fresh.exists()
    assert existing.read_text() == "human-owned"


def test_edited_review_artifact_is_detected_by_check(tmp_path):
    path = tmp_path / "review.md"
    path.write_text("changed")
    with pytest.raises(ValueError, match="generated_review_material_differs"):
        write_outputs({path: b"expected"}, check=True)
    assert path.read_text() == "changed"
