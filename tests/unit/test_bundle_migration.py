"""Review authority survives equivalent format migration, never corpus changes."""

import hashlib
import json
from copy import deepcopy

import pytest

from inferyard.analysis.scoring import score_case, unscorable
from inferyard.config.bundle import content_hash, require_review, upgrade_legacy_bundle
from inferyard.contracts.validation import ContractError, validate_document


def legacy_bundle():
    data = {
        "schema_version": 1,
        "bundle_id": "reviewed",
        "version": "1",
        "language": "zh-CN",
        "license_note": "fixture",
        "review_records": [],
        "answer_policy": dict(
            instruction_newlines="crlf_to_lf",
            strip_line_edges=True,
            ignore_empty_lines=True,
            unicode_normalization="none",
            reasoning="separate_channel_excluded_content_unchanged",
        ),
        "cases": [
            dict(
                case_id="c1",
                category="instruction",
                prompt="只回答是",
                reference_answer="是",
                rules=dict(literal_contains=["是"], literal_forbidden=[], nonempty_line_count=1),
            )
        ],
    }
    data["review_records"] = [
        dict(
            reviewer="human",
            reviewed_at="2026-10-01",
            content_sha256=content_hash(data),
            conclusion="approved",
        )
    ]
    return data


def test_format_upgrade_preserves_original_text_review_and_scoring():
    source = legacy_bundle()
    original = deepcopy(source)
    raw = json.dumps(source, ensure_ascii=False, indent=2) + "\n"
    current = upgrade_legacy_bundle(raw)
    assert source == original
    assert current["schema_version"] == 3
    assert "inherited_bundles" not in current
    assert current["review_records"] == source["review_records"]
    assert current["review_provenance"][0]["source_json"] == raw
    require_review(current)
    score = score_case(current["cases"][0], "是", current["answer_policy"])
    assert score["schema_version"] == 3 and score["quality_state"] == "pass"
    assert len(score["constraint_results"]) == 2
    validate_document("score", unscorable("instruction", "scorer_failure"))


@pytest.mark.parametrize(
    "field",
    ["prompt", "reference_answer", "rules", "policy", "license", "review", "source", "hash"],
)
def test_proof_rejects_changed_content_source_or_removed_original_approval(field):
    current = upgrade_legacy_bundle(legacy_bundle())
    if field in ("prompt", "reference_answer"):
        current["cases"][0][field] += " changed"
    elif field == "rules":
        current["cases"][0]["rules"]["literal_contains"] = ["否"]
    elif field == "policy":
        current["answer_policy"]["strip_line_edges"] = False
    elif field == "license":
        current["license_note"] += " changed"
    elif field == "review":
        current["review_records"] = []
    elif field == "source":
        current["review_provenance"][0]["source_json"] += " "
    else:
        current["review_provenance"][0]["source_content_sha256"] = "f" * 64
    with pytest.raises(ContractError):
        require_review(current)


def test_unreviewed_source_can_migrate_for_diagnostics_but_cannot_gain_approval():
    source = legacy_bundle()
    source["review_records"] = []
    current = upgrade_legacy_bundle(source)
    validate_document("bundle", current)
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(current)


def test_updated_source_hash_cannot_hide_content_that_no_longer_matches_original_review():
    current = upgrade_legacy_bundle(legacy_bundle())
    proof = current["review_provenance"][0]
    source = json.loads(proof["source_json"])
    source["cases"][0]["reference_answer"] = "否"
    current["cases"] = deepcopy(source["cases"])
    proof["source_json"] = json.dumps(source, ensure_ascii=False)
    proof["source_sha256"] = hashlib.sha256(proof["source_json"].encode()).hexdigest()
    proof["source_content_sha256"] = content_hash(source)
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(current)


def test_legacy_json_rejects_duplicate_keys_and_unknown_fields():
    source = legacy_bundle()
    text = json.dumps(source)
    with pytest.raises(ContractError, match="duplicate JSON"):
        upgrade_legacy_bundle('{"schema_version":1,' + text[1:])
    source["automatic_approval"] = True
    with pytest.raises(ContractError, match="unsupported legacy"):
        upgrade_legacy_bundle(source)


def test_v2_inheritance_requires_unchanged_cases_policy_and_both_original_approvals():
    ancestor = legacy_bundle()
    source = {
        **deepcopy(ancestor),
        "schema_version": 2,
        "inherited_bundles": [ancestor],
        "task_protocol": "quality",
        "review_records": [],
    }
    source["review_records"] = [
        dict(
            reviewer="human-2",
            reviewed_at="2026-10-01",
            content_sha256=content_hash(source),
            conclusion="approved",
        )
    ]
    require_review(upgrade_legacy_bundle(source))
    source["inherited_bundles"][0]["review_records"] = []
    current = upgrade_legacy_bundle(source)
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(current)
    source["cases"][0]["prompt"] += " changed"
    with pytest.raises(ContractError, match="inherited case"):
        upgrade_legacy_bundle(source)
