"""Preserve consumer errors for real but unsealed legacy observations."""

import pytest

from inferyard.analysis.overhead_environment import environment_record
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.lab_parameters import verify_parameters
from inferyard.evidence.storage import EvidenceError, json_bytes, read_json
from inferyard.evidence.token_budgets import read_budgets
from inferyard.evidence.trial_evidence import TrialEvidence
from tests.unit.test_layout_consumers import synthetic_pair


def test_explicit_legacy_layout_is_not_an_accepted_declaration(tmp_path):
    old, _ = synthetic_pair(tmp_path)
    manifest = read_json(old / "manifest.json")
    manifest["evidence_layout_version"] = 1
    (old / "manifest.json").write_bytes(json_bytes(manifest))
    with pytest.raises(UnsupportedFormat) as error:
        TrialEvidence(old)
    assert error.value.saved == 1 and error.value.supported == [2]


def test_token_budget_unsealed_observation_keeps_domain_error(tmp_path):
    old, _ = synthetic_pair(tmp_path, tokens=True)
    manifest = read_json(old / "manifest.json")
    del manifest["files"]["token-budgets.v2.json"]
    (old / "manifest.json").write_bytes(json_bytes(manifest))
    config = read_json(old / "config.frozen.json")
    selected = read_json(old / "selection.json")["case_ids"]
    with pytest.raises(EvidenceError, match="^token_budget_evidence_unsealed$"):
        read_budgets(old, config, selected, manifest=manifest["files"])


def test_lab_probe_unsealed_observation_keeps_domain_error(tmp_path):
    old, _ = synthetic_pair(tmp_path)
    (old / "effective-ordinary.json").write_bytes(json_bytes({"unsealed": True}))
    manifest = read_json(old / "manifest.json")
    config = read_json(old / "config.frozen.json")
    events = [
        {
            "event_type": "request_finished",
            "phase": "probe",
            "request_id": "p",
            "data": {"execution_state": "completed"},
        }
    ]
    with pytest.raises(EvidenceError, match="^lab_parameter_evidence_unsealed$"):
        verify_parameters(old, config, {}, events, {"p": {"stream": False}}, {}, manifest["files"])


def test_environment_record_missing_manifest_is_evidence_error(tmp_path):
    old, _ = synthetic_pair(tmp_path)
    (old / "manifest.json").unlink()
    with pytest.raises(EvidenceError, match="invalid_json_evidence"):
        environment_record(old, {})
