"""Commands validate original bytes and physical/member references independently."""

from copy import deepcopy

import pytest

from inferyard.evidence.storage import EvidenceError
from inferyard.evidence.trial_evidence import TrialEvidence
from inferyard.reporting.comparison_report import comparison_input, verify_metric_references
from inferyard.reporting.report import verify_report, write_report
from tests.unit.test_layout_surfaces import sources as sources


@pytest.mark.parametrize("layout", [1, 2])
def test_read_bound_metric_validation_rejects_wrong_references(sources, layout):
    root = sources[layout - 1]
    metadata = {}
    data, _ = comparison_input(root, metadata=metadata)
    metrics = deepcopy(data["summary"]["metric_observations"])
    ref = next(ref for metric in metrics for ref in metric["evidence_refs"])
    ref["sha256"] = "0" * 64
    with pytest.raises(EvidenceError, match="evidence_reference_hash_mismatch"):
        verify_metric_references(root, metrics, reads=metadata["evidence"])
    if layout == 2:
        evidence = metadata["evidence"]
        ref = evidence.input_reference("config")
        ref["member"]["sha256"] = "0" * 64
        with pytest.raises(EvidenceError, match="evidence_reference_member_mismatch"):
            verify_metric_references(root, [{"evidence_refs": [ref]}], reads=evidence)


@pytest.mark.parametrize("layout", [1, 2])
def test_next_command_rechecks_changed_original_bytes(sources, tmp_path, layout):
    root = sources[layout - 1]
    out = tmp_path / "report"
    write_report([root], out)
    assert verify_report(out)["verified"]
    with (root / "events.jsonl").open("ab") as stream:
        stream.write(b"\n")
    with pytest.raises(EvidenceError, match="original_evidence_hash_mismatch"):
        verify_report(out)


def test_nested_member_references_share_reader_without_rewriting_paths(sources):
    _, root = sources
    evidence = TrialEvidence(root)
    refs = [evidence.observation_reference("environment", stage=s) for s in ("start", "end")]
    for ref in refs:
        ref["path"] = root.name + "/" + ref["path"]
    verify_metric_references(root.parent, [{"evidence_refs": refs}])
    invalid = deepcopy(refs[0])
    invalid["path"] = root.name + "/../" + invalid["path"]
    with pytest.raises(EvidenceError, match="invalid_evidence_reference"):
        verify_metric_references(root.parent, [{"evidence_refs": [invalid]}])
