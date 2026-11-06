import json
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.analysis.scoring import score_case

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
