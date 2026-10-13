"""Independent known-answer and denominator checks for the six quality tasks."""

import copy
import json
from pathlib import Path

import pytest

from inferyard.analysis.quality import summarize_quality
from inferyard.analysis.scoring import score_case, unscorable
from inferyard.config.bundle import content_hash, require_review, validate_case
from inferyard.contracts.validation import ContractError, validate_document

BUNDLE = json.loads(Path("bundles/zh-smoke.json").read_text())
POLICY = BUNDLE["answer_policy"]


def case(category, rules, case_id="new-01"):
    return dict(case_id=case_id, category=category, prompt="测试", reference_answer="", rules=rules)


def scored(task, answer, state="completed"):
    return dict(
        case_id=task["case_id"], execution_state=state, score=score_case(task, answer, POLICY)
    )


def test_original_forty_reference_answers_remain_passing():
    for task in BUNDLE["cases"]:
        assert score_case(task, task["reference_answer"], POLICY)["quality_state"] == "pass"


@pytest.mark.parametrize(
    "answer,normalization,passed",
    [
        (" 答案 ", "strip", True),
        ("答案。", "strip", False),
        ("Ａ", "nfkc_strip", True),
        ("Ａ", "strip", False),
    ],
)
def test_exact_answer_normalization(answer, normalization, passed):
    task = case("qa", dict(answers=["答案", "A"], normalization=normalization))
    assert (score_case(task, answer, POLICY)["quality_state"] == "pass") == passed


@pytest.mark.parametrize(
    "answer,passed",
    [
        ("10米", True),
        ("10.1 米", True),
        ("9.9米", True),
        ("10.10001米", False),
        ("10", False),
        ("答案是10米", False),
        ("NaN米", False),
        ("true米", False),
        ("1e999999999米", False),
        ("1e-999999999米", False),
    ],
)
def test_numeric_tolerance_and_hostile_exponents(answer, passed):
    task = case("math", dict(expected=10, absolute_tolerance=0.1, relative_tolerance=0, unit="米"))
    assert (score_case(task, answer, POLICY)["quality_state"] == "pass") == passed


@pytest.mark.parametrize(
    "expected,absolute,relative,answer,passed",
    [
        (0, 0, 0, "1e-999999999", False),
        (0, 0, 0, "-1e-999999999", False),
        (0, 0, 0, "0e-999999999", True),
        (0, 1e-300, 0, "1e-999999999", True),
        (0, 0, 0, "1e999999999", False),
        (10**40, 0, 0, str(10**40 + 1), False),
        (10**40, 1, 0, str(10**40 + 1), True),
        (10**40, 1, 0, str(10**40 + 2), False),
        (1, 1e-30, 0, "1.000000000000000000000000000001", True),
        (1, 1e-30, 0, "1.000000000000000000000000000002", False),
        (-10, 0, 0.01, "-10.1", True),
        (-10, 0, 0.01, "-10.100000000000000000000000001", False),
    ],
)
def test_numeric_bounds_are_exact(expected, absolute, relative, answer, passed):
    task = case(
        "math",
        dict(expected=expected, absolute_tolerance=absolute, relative_tolerance=relative, unit=""),
    )
    assert (score_case(task, answer, POLICY)["quality_state"] == "pass") == passed


def structured(case_id="struct-01"):
    return case(
        "structured",
        dict(
            json_schema={
                "type": "object",
                "required": ["data"],
                "additionalProperties": False,
                "properties": {"data": {"type": "array", "minItems": 3, "maxItems": 3}},
            },
            expected={"data": [True, 2, None]},
            fields=["/data/0", "/data/1", "/data/2"],
            array_matching="ordered",
        ),
        case_id,
    )


@pytest.mark.parametrize(
    "answer,expected",
    [
        ('{"data":[true,2,null]}', [True, True, True]),
        ('{"data":[1,2,null]}', [False, True, True]),
        ('{"data":[true,null,2]}', [True, False, False]),
        ('{"data":[true,2,null],"data":[]}', [False, False, False]),
        ('{"data":[NaN,2,null]}', [False, False, False]),
        ('```json\n{"data":[true,2,null]}\n```', [False, False, False]),
    ],
)
def test_nested_typed_fields_and_strict_json(answer, expected):
    result = score_case(structured(), answer, POLICY)
    assert [f["passed"] for f in result["field_results"]] == expected
    assert (result["quality_state"] == "pass") == all(expected)


def test_parse_failure_and_partial_correct_fields_keep_denominators():
    a, b = structured("a"), structured("b")
    metrics = summarize_quality(
        [a, b], [scored(a, '{"data":[false,2,null]}'), scored(b, "not JSON")], complete=True
    )
    assert metrics["Q03"]["value"] == 0.5
    assert metrics["Q04"]["value"] == 0.5
    assert metrics["Q05"]["numerator"] == 2
    assert metrics["Q05"]["denominator"] == 6
    assert metrics["Q01"]["structured"]["rate"]["value"] == 0


def test_failed_partial_answer_never_passes_quality():
    task = structured()
    metrics = summarize_quality(
        [task], [scored(task, '{"data":[true,2,null]}', "failed")], complete=True
    )
    assert metrics["Q05"]["numerator"] == 0
    assert metrics["Q05"]["denominator"] == 3
    assert metrics["Q03"]["value"] == 0


def test_classification_macro_f1_includes_invalid_and_failed_answers():
    tasks = [
        case("classification", dict(labels=["A", "B"], expected=label), str(i))
        for i, label in enumerate(["A", "A", "B", "B"])
    ]
    requests = [
        scored(t, answer, state)
        for t, answer, state in zip(
            tasks, ["A", "invalid", "B", "B"], ["completed"] * 3 + ["failed"], strict=True
        )
    ]
    metrics = summarize_quality(tasks, requests, complete=True)
    assert metrics["Q08"]["value"] == pytest.approx(2 / 3)
    assert metrics["Q08"]["sample_count"] == 4
    assert metrics["Q01"]["classification"]["rate"]["value"] == 0.5


def test_missing_duplicate_and_wrong_category_cannot_form_complete_trial():
    task = structured()
    metrics = summarize_quality([task], [], complete=True)
    assert metrics["Q05"]["value"] is None
    assert metrics["Q01"]["structured"]["excluded"] == 1
    request = scored(task, '{"data":[true,2,null]}')
    with pytest.raises(ContractError, match="duplicate"):
        summarize_quality([task], [request, request], complete=True)
    request["score"] = unscorable("qa", "missing_answer")
    with pytest.raises(ContractError, match="category"):
        summarize_quality([task], [request], complete=True)


def test_missing_field_scores_rejected_and_unscorable_is_incomplete():
    task = structured()
    request = scored(task, '{"data":[true,2,null]}')
    request["score"]["field_results"] = []
    with pytest.raises(ContractError, match="denominator"):
        summarize_quality([task], [request], complete=True)
    request["score"] = unscorable("structured", "answer_redacted")
    assert summarize_quality([task], [request], complete=True)["Q05"]["value"] is None


def test_remote_schema_and_invalid_reference_rejected_before_scoring():
    task = structured()
    task["rules"]["json_schema"] = {"$ref": "https://example.invalid/schema"}
    with pytest.raises(ContractError, match="references"):
        validate_case(task)
    task = structured()
    task["rules"]["expected"] = []
    with pytest.raises(ContractError, match="reference violates"):
        validate_case(task)


def test_inherited_approval_is_preserved_but_new_composition_needs_review():
    source = json.loads(BUNDLE["review_provenance"][0]["source_json"])
    assert content_hash(source) == BUNDLE["review_records"][0]["content_sha256"]
    require_review(BUNDLE)
    bundle = copy.deepcopy(BUNDLE)
    bundle.pop("review_provenance")
    bundle["review_records"] = []
    bundle["cases"].append(case("qa", dict(answers=["答案"], normalization="strip")))
    validate_document("bundle", bundle)
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)
    changed = copy.deepcopy(BUNDLE)
    changed["cases"][0]["prompt"] += "修改"
    with pytest.raises(ContractError, match="current content differs"):
        validate_document("bundle", changed)


def test_removing_current_approval_does_not_reactivate_legacy_proof():
    subset = copy.deepcopy(BUNDLE)
    subset["review_records"] = []
    with pytest.raises(ContractError, match="original review record was removed"):
        require_review(subset)


def test_performance_has_no_quality_score():
    task = case("performance", {"output_target_tokens": None})
    assert score_case(task, "", POLICY)["quality_state"] == "not_applicable"
    assert unscorable("performance", "not_needed")["quality_state"] == "not_applicable"
