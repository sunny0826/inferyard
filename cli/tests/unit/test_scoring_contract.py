"""Independent known-answer and denominator checks for quality scoring."""

import json
from pathlib import Path

import pytest

from inferyard.analysis.quality import summarize_quality
from inferyard.analysis.scoring import score_case

BUNDLE = json.loads(Path("bundles/zh-smoke.json").read_text())
POLICY = BUNDLE["answer_policy"]


def case(category, rules, case_id="new-01"):
    return dict(case_id=case_id, category=category, prompt="测试", reference_answer="", rules=rules)


def scored(task, answer, state="completed"):
    return dict(
        case_id=task["case_id"], execution_state=state, score=score_case(task, answer, POLICY)
    )


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
