"""Adapter-bound service property stages on newly generated offline fixtures."""

import pytest

from inferyard.evidence.error_reasons import safe_reason
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, json_bytes, read_jsonl
from inferyard.evidence.trial_evidence import TrialEvidence
from tests.evidence_layout_helpers import seal_layout
from tests.lab_service import lab_config
from tests.unit.test_layout_consumers import bind_frozen_config, synthetic_pair


def change_stage(root, stage):
    run = TrialEvidence(root).input("run")
    rows, _ = read_jsonl(root / "observations.jsonl")
    next(row for row in rows if row["kind"] == "service_properties")["stage"] = stage
    (root / "observations.jsonl").write_bytes(b"".join(map(json_bytes, rows)))
    seal_layout(root, run)


def test_read_trial_rejects_prism_properties_saved_before_idle(tmp_path):
    old, root = synthetic_pair(tmp_path, tokens=True)
    original = {path.name: path.read_bytes() for path in old.iterdir()}
    before = read_trial(root)
    assert before["summary"]["completeness"] == "complete"
    assert all(request["engine_build_verified"] for request in before["requests"])
    change_stage(root, "pre_idle")
    with pytest.raises(EvidenceError) as failure:
        read_trial(root)
    assert str(failure.value) == "service_properties_stage_mismatch"
    assert safe_reason(failure.value, "offline_error") == "service_properties_stage_mismatch"
    assert original == {path.name: path.read_bytes() for path in old.iterdir()}


@pytest.mark.parametrize("adapter", ["prism_llama_server_v1", "kvmem", "ninfer"])
@pytest.mark.parametrize("projection", [False, True])
@pytest.mark.parametrize("correct_stage", [False, True])
def test_scanning_any_observation_binds_properties_stage_to_adapter(
    tmp_path, adapter, projection, correct_stage
):
    _, root = synthetic_pair(tmp_path, tokens=True)
    config = TrialEvidence(root).input("config")
    if adapter != "prism_llama_server_v1":
        config = lab_config(config, adapter)
        bind_frozen_config(root, config)
    expected = "post_idle" if adapter == "prism_llama_server_v1" else "pre_idle"
    actual = expected if correct_stage else "pre_idle" if expected == "post_idle" else "post_idle"
    change_stage(root, actual)
    evidence = TrialEvidence(root, projection=projection)
    if correct_stage:
        assert evidence.observation("environment", "start") is not None
        assert evidence.observation("service_properties", expected) is not None
        assert (
            evidence.observation(
                "service_properties", "post_idle" if expected == "pre_idle" else "pre_idle"
            )
            is None
        )
    else:
        with pytest.raises(EvidenceError) as failure:
            evidence.observation("environment", "start")
        assert str(failure.value) == "service_properties_stage_mismatch"


@pytest.mark.parametrize("projection", [False, True])
def test_layout1_properties_keep_the_original_support_boundary(tmp_path, projection):
    root, _ = synthetic_pair(tmp_path, tokens=True)
    original = {path.name: path.read_bytes() for path in root.iterdir()}
    evidence = TrialEvidence(root, projection=projection)
    assert evidence.layout_version == 1
    assert evidence.observation("service_properties") is not None
    assert evidence.observation("service_properties", "post_idle") is not None
    assert evidence.observation("service_properties", "pre_idle") is None
    assert read_trial(root)["summary"]["completeness"] == "complete"
    assert original == {path.name: path.read_bytes() for path in root.iterdir()}
