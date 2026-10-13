"""Independent baseline reads never become run evidence or broaden diagnostics."""

import pytest

from inferyard.evidence.baseline_reads import BaselineEvidence, environment_reads
from inferyard.evidence.error_reasons import safe_reason
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.evidence.trial_evidence import TrialEvidence, verify_reference
from tests.helpers import fixture_run


def test_baseline_domain_refs_bind_original_sensor_bytes(tmp_path):
    (tmp_path / "environment.start.json").write_bytes(json_bytes({"platform": "Linux"}))
    (tmp_path / "boundary-observer.json").write_bytes(json_bytes({"collector": "synthetic"}))
    (tmp_path / "request-environment.jsonl").write_bytes(json_bytes({"boundary": "before_send"}))
    originals = {p.name: p.read_bytes() for p in tmp_path.iterdir()}
    reads = environment_reads(tmp_path)
    assert isinstance(reads, BaselineEvidence)
    assert reads.observation("environment", stage="start") == {"platform": "Linux"}
    assert not reads.has_observation("environment", stage="end")
    assert reads.observation_reference("environment", stage="end") is None
    assert reads.has_observation("boundary_observer", stage="prepare")
    assert reads.checked_jsonl("request-environment.jsonl") == ([{"boundary": "before_send"}], [])
    for ref in (
        reads.observation_reference("environment", stage="start"),
        reads.observation_reference("boundary_observer"),
        reads.log_reference("request-environment.jsonl"),
    ):
        assert verify_reference(tmp_path, ref) is None
    assert originals == {p.name: p.read_bytes() for p in tmp_path.iterdir()}
    with pytest.raises(EvidenceError, match="run_evidence_missing"):
        TrialEvidence(tmp_path)


@pytest.mark.parametrize("name", ["events.jsonl", "memory.jsonl", "config.input.json"])
def test_baseline_rejects_run_inventory_and_identifies_strictly(tmp_path, name):
    (tmp_path / "environment.start.json").write_bytes(json_bytes({"platform": "Linux"}))
    (tmp_path / name).write_bytes(b"")
    with pytest.raises(EvidenceError, match="baseline_evidence_contains_run"):
        BaselineEvidence(tmp_path)
    with pytest.raises(EvidenceError, match="run_evidence_missing"):
        environment_reads(tmp_path)


def test_run_selection_uses_strict_domain_reader(tmp_path):
    root = fixture_run(tmp_path)
    assert isinstance(environment_reads(root), TrialEvidence)
    with pytest.raises(EvidenceError, match="baseline_evidence_contains_run"):
        BaselineEvidence(root)


def test_baseline_queries_cannot_read_inputs_or_requests(tmp_path):
    reads = BaselineEvidence(tmp_path)
    with pytest.raises(EvidenceError, match="invalid_baseline_observation_query"):
        reads.observation("identity")
    with pytest.raises(EvidenceError, match="invalid_baseline_log_query"):
        reads.checked_jsonl("request-bodies.jsonl")


def test_new_codec_failures_keep_exact_public_reasons(tmp_path):
    with pytest.raises(EvidenceError) as failure:
        TrialEvidence(tmp_path)
    assert safe_reason(failure.value, "offline_error") == "run_evidence_missing"
    reference = {"path": "environment.start.json", "sha256": "a" * 64}
    with pytest.raises(EvidenceError) as failure:
        verify_reference(tmp_path, reference)
    assert safe_reason(failure.value, "offline_error") == "evidence_reference_missing"
    assert safe_reason(EvidenceError("run_evidence_missing:private-path"), "offline_error") == (
        "offline_error"
    )
