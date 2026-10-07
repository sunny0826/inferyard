"""SVG is display-only: extraction never repairs or sanitizes model output."""

from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis.quality import summarize_quality
from inferyard.analysis.scoring import score_case, unscorable
from inferyard.config.bundle import require_review, validate_case
from inferyard.contracts.validation import ContractError, Document
from inferyard.evidence.storage import read_json
from inferyard.reporting.svg_render import MAX_SVG_CHARACTERS, check_svg, extract_svg

BUNDLE = read_json(Path("bundles/zh-svg-pelican.json"))
SVG = '<svg xmlns="http://www.w3.org/2000/svg"><text>鹈鹕</text></svg>'


@pytest.mark.parametrize(
    "content,expected",
    [
        (f"before\n```svg\n{SVG}\n```\nafter", SVG + "\n"),
        (f"before {SVG} after", SVG),
        (SVG + '<svg id="second"></svg>', SVG),
        ('<svg id="first"></svg>\n```svg\n' + SVG + "\n```", SVG + "\n"),
        ("<svg><path", None),
        ("nothing here", None),
        ("```svg\n<svg><path\n```", "<svg><path\n"),
        ("```svg\n<svg><path", None),
    ],
)
def test_extract_svg_is_deterministic(content, expected):
    assert extract_svg(content) == expected
    assert check_svg(extract_svg(content)) == check_svg(extract_svg(content))


@pytest.mark.parametrize(
    "text,status",
    [
        (None, "not_found"),
        ("", "malformed"),
        ("<svg><g></svg>", "malformed"),
        ("<svg><path", "malformed"),
        ("<html></html>", "malformed"),
        ('<!DOCTYPE svg [<!ENTITY x "hello">]><svg>&x;</svg>', "malformed"),
        ('<!DOCTYPE svg SYSTEM "file:///etc/passwd"><svg></svg>', "malformed"),
        ('<!DOCTYPE svg SYSTEM "https://example.invalid/a"><svg></svg>', "malformed"),
        (SVG, "ok"),
        ("<svg/>", "ok"),
    ],
)
def test_check_svg_status_and_original_utf8_bytes(text, status):
    result = check_svg(text)
    assert result == {
        "status": status,
        "svg": text if status == "ok" else None,
        "bytes": len(text.encode()) if text is not None else 0,
    }


def test_character_limit_is_inclusive_and_distinct_from_utf8_byte_count():
    text = "<svg>" + "鹈" * (MAX_SVG_CHARACTERS - 11) + "</svg>"
    assert len(text) == MAX_SVG_CHARACTERS
    assert check_svg(text)["status"] == "ok"
    assert check_svg(text)["bytes"] > MAX_SVG_CHARACTERS
    result = check_svg(text + " ")
    assert result == {"status": "too_large", "svg": None, "bytes": len((text + " ").encode())}


def test_raw_styles_scripts_and_attributes_are_never_sanitized():
    text = (
        '<svg xmlns="http://www.w3.org/2000/svg" onload="window.pwned=true">'
        "<style>@keyframes cycle{to{opacity:.5}}</style><script>window.pwned=true</script>"
        '<foreignObject><div xmlns="http://www.w3.org/1999/xhtml">text</div></foreignObject>'
        "</svg>"
    )
    assert check_svg(extract_svg(text))["svg"] == text


def test_pelican_bundle_is_valid_and_human_reviewed():
    Document.parse("bundle", BUNDLE)
    assert BUNDLE["cases"][0]["prompt"] == (
        "Generate an animated SVG of a pelican riding a bicycle"
    )
    from inferyard.config.bundle import content_hash

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
    from inferyard.analysis.position import validate_position_cases

    task = BUNDLE["cases"][0]
    workload = {
        "purpose": "position",
        "protocol": {"case_ids": [task["case_id"]]},
        "position_cases": [{"case_id": task["case_id"]}],
    }
    with pytest.raises(ContractError, match="requires a quality-scored case"):
        validate_position_cases(workload, BUNDLE)
