"""Historical JSON is retained only as a current bundle's immutable review proof."""

import copy
import json
from pathlib import Path

import pytest

from inferyard.config.bundle import require_review, validate_bundle
from inferyard.contracts.validation import ContractError


def approved_bundle():
    return json.loads((Path(__file__).parents[2] / "bundles/zh-smoke.json").read_text())


def test_bundled_review_proof_is_still_approved():
    require_review(approved_bundle())


@pytest.mark.parametrize("change", ["hash", "source", "content", "review", "duplicate"])
def test_proof_tampering_never_grants_review(change):
    bundle = copy.deepcopy(approved_bundle())
    proof = bundle["review_provenance"][0]
    if change == "hash":
        proof["source_sha256"] = "0" * 64
    elif change == "source":
        proof["source_json"] += " "
    elif change == "content":
        bundle["cases"][0]["prompt"] += "changed"
    elif change == "review":
        bundle["review_records"] = []
    else:
        bundle["review_provenance"].append(copy.deepcopy(proof))
    with pytest.raises(ContractError):
        validate_bundle(bundle)


def test_migration_generation_api_is_absent():
    from inferyard.config import bundle, bundle_review

    assert not hasattr(bundle, "upgrade_legacy_bundle")
    assert not hasattr(bundle_review, "upgrade_legacy_bundle")
