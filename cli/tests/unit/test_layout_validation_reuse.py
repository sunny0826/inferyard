"""Repeated references retain rejection of altered member identities and shapes."""

from copy import deepcopy

import pytest

from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.reporting.comparison_report import verify_metric_references
from tests.layout_mutations import change_input
from tests.unit.test_layout_surfaces import sources as sources


@pytest.mark.parametrize(
    "damage,reason",
    [
        ("extra", "invalid_evidence_reference"),
        ("member_extra", "invalid_evidence_reference"),
        ("bool_seq", "invalid_evidence_reference"),
        ("float_seq", "invalid_evidence_reference"),
        ("null_member", "invalid_evidence_reference"),
        ("hash", "evidence_reference_member_mismatch"),
    ],
)
def test_prior_valid_reference_does_not_hide_altered_reference(sources, damage, reason):
    _, root = sources
    reads = TrialEvidence(root)
    ref = reads.observation_reference("environment", stage="start")
    assert ref["member"]["seq"] == 1
    bad = deepcopy(ref)
    if damage == "extra":
        bad["extra"] = {"nested": True}
    elif damage == "member_extra":
        bad["member"]["extra"] = "unexpected"
    elif damage == "bool_seq":
        bad["member"]["seq"] = True
    elif damage == "float_seq":
        bad["member"]["seq"] = 1.0
    elif damage == "null_member":
        bad["member"] = None
    else:
        bad["member"]["sha256"] = "0" * 64
    with pytest.raises(EvidenceError, match=reason):
        verify_metric_references(root, [{"evidence_refs": [ref, deepcopy(ref), bad]}], reads=reads)


@pytest.mark.parametrize("kind", ["run", "plan", "selection", "config", "bundle"])
def test_container_inputs_still_validate_nested_documents_before_reduction(sources, kind):
    _, root = sources
    change_input(root, kind, lambda data: data.update(unexpected=True), sealed=True)
    with pytest.raises(EvidenceError, match="invalid_inputs_container"):
        read_trial(root)
