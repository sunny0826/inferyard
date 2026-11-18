"""Purpose-specific B contracts; synthetic observations only."""

from copy import deepcopy

import pytest

from inferyard.analysis.comparison import compare_trials
from inferyard.contracts.bundle import content_hash
from inferyard.config.bundle import require_review
from inferyard.contracts.bundle_review import approved_cases, case_content_hash
from inferyard.contracts.environment_admission import admission
from inferyard.contracts.validation import Document, json_bytes
from inferyard.errors import ContractError, PreflightError, UnsupportedFormat
from inferyard.analysis.comparison_input import comparison_input
from inferyard.reporting.comparison_report import build_comparison, read_verified_comparison
from inferyard.runtime.service_reuse import require_transition
from tests.helpers import fixture_run


@pytest.fixture
def pair(tmp_path):
    return [comparison_input(fixture_run(tmp_path / side))[0] for side in ("a", "b")]


def test_case_review_only_invalidates_affected_content(pair):
    bundle = deepcopy(pair[0]["bundle"])
    bundle["review_records"] = []
    bundle["case_review_records"] = [
        dict(
            definition="case-review.v1",
            case_id=c["case_id"],
            content_sha256=case_content_hash(bundle, c),
            reviewer="human",
            reviewed_at="2026-10-07",
            conclusion="approved",
        )
        for c in bundle["cases"]
    ]
    require_review(bundle)
    bundle["license_note"] = "new attribution"
    bundle["version"] = "display-version"
    require_review(bundle)
    original = deepcopy(bundle)
    bundle["cases"][0]["prompt"] += " new"
    assert approved_cases(bundle) == {c["case_id"] for c in bundle["cases"][1:]}
    with pytest.raises(ContractError, match="human_corpus_review_required"):
        require_review(bundle)
    bundle["case_review_records"].append(
        {
            **bundle["case_review_records"][0],
            "content_sha256": case_content_hash(bundle, bundle["cases"][0]),
        }
    )
    require_review(bundle)
    bundle["answer_policy"]["strip_line_edges"] = not bundle["answer_policy"]["strip_line_edges"]
    assert approved_cases(bundle) == set()
    original["case_review_records"][0]["extra"] = True
    with pytest.raises(ContractError):
        Document.parse("bundle", original)


@pytest.mark.parametrize("engine", ["prism", "kvmem", "ninfer"])
def test_same_process_without_drain_proof_is_explicitly_refused(pair, engine):
    previous = pair[0]
    previous["config"]["engine"]["adapter"] = engine
    with pytest.raises(PreflightError, match="service_reuse_drain_evidence_missing"):
        require_transition(previous, previous["config"], lambda _: None, reason="fresh")
