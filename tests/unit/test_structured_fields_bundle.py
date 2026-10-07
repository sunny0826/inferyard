"""Field clarifications require a new review and retain every scoring rule."""

import subprocess
import sys
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard.config.bundle import require_review
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import read_json
from scripts.build_structured_fields_bundle import FACTS, SOURCE, build


def test_two_field_prompts_preserve_all_other_cases_rules_and_order():
    source = read_json(SOURCE)
    before = deepcopy(source)
    candidate, changes = build(source)
    assert source == before
    assert {c["case_id"] for c in changes} == set(FACTS)
    assert candidate["answer_policy"] == source["answer_policy"]
    for old, new in zip(source["cases"], candidate["cases"], strict=True):
        assert {k: v for k, v in old.items() if k != "prompt"} == {
            k: v for k, v in new.items() if k != "prompt"
        }
        if old["case_id"] not in FACTS:
            assert old == new
        else:
            assert old["prompt"].partition("输出")[2] == new["prompt"].partition("输出")[2]


def test_current_root_clarification_approval_cannot_approve_new_field_meanings():
    source = read_json(SOURCE)
    require_review(source)
    candidate, _ = build(source)
    assert not candidate["review_records"]
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(candidate)
    candidate["review_records"] = source["review_records"]
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(candidate)


def test_documented_direct_script_check_works_outside_repository_cwd(tmp_path):
    script = Path(__file__).resolve().parents[2] / "scripts/build_structured_fields_bundle.py"
    result = subprocess.run(
        [sys.executable, str(script), "--check"],
        cwd=tmp_path,
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr
