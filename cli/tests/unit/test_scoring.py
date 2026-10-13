import json
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis.scoring import score_case
from inferyard.contracts.validation import validate_document

ROOT = Path(__file__).parents[2]
FIXTURES = json.loads((ROOT / "tests/fixtures/scoring.json").read_text())


@pytest.mark.parametrize("example", FIXTURES)
def test_independently_prespecified_scoring_examples(example, wire_fixture):
    bundle = wire_fixture("bundle")
    case = deepcopy(bundle["cases"][0 if example["kind"] == "instruction" else 1])
    if example["kind"] == "instruction":
        case["rules"] = {
            "literal_contains": example["contains"],
            "literal_forbidden": example["forbidden"],
            "nonempty_line_count": example["lines"],
        }
    score = score_case(case, example["answer"], bundle["answer_policy"])
    assert (score["quality_state"] == "pass") is example["pass"]
    if "format" in example:
        assert score["format_ok"] is example["format"]


def test_draft_corpus_shape_and_reference_sanity_are_not_human_review():
    bundle = json.loads((ROOT / "bundles/zh-smoke.json").read_text())
    validate_document("bundle", bundle)
    assert len(bundle["cases"]) == 40
    assert sum(c["category"] == "instruction" for c in bundle["cases"]) == 20
    assert sum(c["category"] == "extraction" for c in bundle["cases"]) == 20
    # Independent fixtures above validate the scorer; this only checks draft consistency.
    for case in bundle["cases"]:
        assert (
            score_case(case, case["reference_answer"], bundle["answer_policy"])["quality_state"]
            == "pass"
        )
