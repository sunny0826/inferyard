"""SVG tasks stay display-only: no quality metric, denominator or score changes."""

from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis.quality import summarize_quality
from inferyard.analysis.scoring import score_case, unscorable
from inferyard.contracts.bundle import validate_case
from inferyard.config.bundle import require_review
from inferyard.contracts.validation import Document
from inferyard.errors import ContractError
from inferyard.evidence.storage import read_json

BUNDLE = read_json(Path("bundles/zh-svg-pelican.json"))
SVG = '<svg xmlns="http://www.w3.org/2000/svg"><text>鹈鹕</text></svg>'


def test_pelican_bundle_is_valid_and_human_reviewed():
    Document.parse("bundle", BUNDLE)
    assert BUNDLE["cases"][0]["prompt"] == (
        "Generate an animated SVG of a pelican riding a bicycle"
    )
    from inferyard.contracts.bundle import content_hash

    (record,) = BUNDLE["review_records"]
    assert record["conclusion"] == "approved"
    assert record["content_sha256"] == content_hash(BUNDLE)
    require_review(BUNDLE)
    unreviewed = deepcopy(BUNDLE)
    unreviewed["review_records"] = []
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(unreviewed)


@pytest.mark.parametrize("rules", [{"unexpected": True}, {"output_target_tokens": None}, [], None])
def test_svg_rejects_nonempty_or_nonobject_rules(rules):
    bundle = deepcopy(BUNDLE)
    bundle["cases"][0]["rules"] = rules
    with pytest.raises(ContractError):
        Document.parse("bundle", bundle)
    with pytest.raises(ContractError):
        validate_case(bundle["cases"][0])


def test_svg_requires_reference_and_quality_protocol():
    bundle = deepcopy(BUNDLE)
    del bundle["cases"][0]["reference_answer"]
    with pytest.raises(ContractError):
        Document.parse("bundle", bundle)
    bundle = deepcopy(BUNDLE)
    bundle["task_protocol"] = "performance"
    with pytest.raises(ContractError, match="mixed quality and performance"):
        Document.parse("bundle", bundle)


@pytest.mark.parametrize("answer", [SVG, "", "<svg>", "not SVG"])
def test_svg_always_not_applicable_and_never_scores_xml(answer):
    score = score_case(BUNDLE["cases"][0], answer, BUNDLE["answer_policy"])
    assert score["quality_state"] == "not_applicable"
    assert score["content_ok"] is score["format_ok"] is None
    assert score["rule_results"] == score["field_results"] == score["constraint_results"] == []
    assert unscorable("svg", "missing_answer")["quality_state"] == "not_applicable"
    score["quality_state"] = "pass"
    with pytest.raises(ContractError, match="not applicable"):
        Document.parse("score", score)


@pytest.mark.parametrize(
    "state", ["completed", "failed", "cancelled", "invalid", "not_executed", None]
)
def test_svg_does_not_change_any_quality_metric_or_denominator(state):
    core = read_json(Path("bundles/zh-core.json"))
    cases = core["cases"]
    rows = [
        {
            "case_id": c["case_id"],
            "execution_state": "completed",
            "score": score_case(c, c["reference_answer"], core["answer_policy"]),
        }
        for c in cases
    ]
    before = summarize_quality(cases, rows, complete=True)
    svg = BUNDLE["cases"][0]
    extra = [{"case_id": svg["case_id"], "execution_state": state}] if state else []
    assert summarize_quality(cases + [svg], rows + extra, complete=True) == before


def test_svg_length_cohort_keeps_timing_without_quality_or_unscorable_counts():
    from inferyard.analysis.input_lengths import input_length_summary
    from tests.unit.test_input_lengths import workload
    from tests.unit.test_observations import EVIDENCE, RUN, examples

    _, rows = examples()
    for row in rows:
        row.update(category="svg", score=None)
    summary, _ = input_length_summary(RUN, workload(), rows, {}, EVIDENCE, complete=True)
    assert summary["bins"][0]["quality_rate"] is None
    assert summary["bins"][0]["unscorable_completed"] == 0
    assert summary["bins"][0]["completed_latency_p50_ms"] == 2000


def test_svg_repeat_cohort_has_no_quality_denominator():
    from inferyard.analysis.repetition_metrics import repetition_metrics
    from tests.unit.test_repetition_metrics import inputs

    plan, runs = inputs()
    for run in runs:
        for row in run["requests"]:
            row.update(category="svg", quality_state="not_applicable")
    group = repetition_metrics(plan, runs)[0]
    assert group["S03"]["value"] is None
    assert group["S03"]["denominator"] == 0
    assert not any(c["quality_applicable"] for c in group["cases"])
    assert group["S04"]["trial_medians_ms"] == [1, 2, 3]


def test_svg_cannot_be_used_as_a_scored_position_probe():
    from inferyard.contracts.position_cases import validate_position_cases

    task = BUNDLE["cases"][0]
    workload = {
        "purpose": "position",
        "protocol": {"case_ids": [task["case_id"]]},
        "position_cases": [{"case_id": task["case_id"]}],
    }
    with pytest.raises(ContractError, match="requires a quality-scored case"):
        validate_position_cases(workload, BUNDLE)
