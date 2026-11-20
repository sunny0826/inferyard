"""Purpose-specific B contracts; synthetic observations only."""

from copy import deepcopy

import pytest

from inferyard.config.bundle import require_review
from inferyard.contracts.validation import Document
from inferyard.errors import ContractError, PreflightError
from inferyard.analysis.comparison_input import comparison_input
from inferyard.runtime.service_reuse import require_transition
from tests.helpers import fixture_run


@pytest.fixture
def pair(tmp_path):
    return [comparison_input(fixture_run(tmp_path / side))[0] for side in ("a", "b")]


def test_bundle_validation_allows_edited_corpus_without_approval_hashes(pair):
    bundle = deepcopy(pair[0]["bundle"])
    bundle.pop("review_records", None)
    bundle.pop("review_provenance", None)
    bundle["cases"][0]["prompt"] += " new"
    require_review(bundle)
    Document.parse("bundle", bundle)
    invalid = deepcopy(bundle)
    invalid["cases"].append(deepcopy(invalid["cases"][0]))
    with pytest.raises(ContractError, match="duplicate case_id"):
        require_review(invalid)


@pytest.mark.parametrize("engine", ["prism", "kvmem", "ninfer"])
def test_same_process_without_drain_proof_is_explicitly_refused(pair, engine):
    previous = pair[0]
    previous["config"]["engine"]["adapter"] = engine
    with pytest.raises(PreflightError, match="service_reuse_drain_evidence_missing"):
        require_transition(previous, previous["config"], lambda _: None, reason="fresh")
