"""Target binding gates with an explicitly mocked sealed control verifier."""

import hashlib

import pytest

import inferyard.evidence.total_control_binding as binding
from inferyard.evidence.storage import EvidenceError, json_bytes
from inferyard.runtime.overhead_runner import request_projection


def test_missing_full_control_cannot_qualify_incremental_evidence(tmp_path):
    assert binding.load_total_binding(tmp_path, tmp_path, {}) == {
        "eligible": False,
        "reasons": ["formal_trial_total_observer_evidence_missing"],
    }


@pytest.mark.parametrize(
    "change,reason",
    [
        ("v1", "protocol_required"),
        ("fixture", "not_qualified"),
        ("output", "output_work_differs"),
        ("source", "tool_mismatch"),
        ("chronology", "not_before_target"),
        ("config", "config_mismatch"),
        ("collector", "collector_mismatch"),
        ("workload", "workload_mismatch"),
    ],
)
def test_full_control_rejects_an_incompatible_target(tmp_path, monkeypatch, change, reason):
    control, target, overhead = [tmp_path / name for name in ("control", "target", "overhead")]
    for path in (control, target, overhead):
        path.mkdir()
    (control / "manifest.json").write_bytes(b"fixture manifest")
    for name in ("config.frozen.json", "bundle.json"):
        (target / name).write_bytes(json_bytes({}))
    row = {
        "case_id": "a",
        "t_send_ns": 100,
        "t_terminal_ns": 200,
        "content": "answer",
        "completion_tokens": 1,
        "token_source": "engine",
        "token_scope": "completion",
        "execution_state": "completed",
    }
    data = {"run": {"tool_source_sha256": "a" * 64}, "requests": [row]}
    packet = {
        "spec": {
            "definition": "total_observer_control.v2",
            "case_ids": ["a"],
            "tool_source_sha256": "a" * 64,
            "config_sha256": hashlib.sha256(json_bytes({})).hexdigest(),
            "bundle_sha256": hashlib.sha256(json_bytes({})).hexdigest(),
            "tolerance_ratio": 0.05,
        },
        "evidence_kind": "live",
        "guard": {"clock_id": "boot:CLOCK_MONOTONIC"},
        "rows": [
            {
                "mode": mode,
                "after_close_ns": 500,
                "trial_path": "child",
                "requests": [{"output_sha256": request_projection(row)["output_identity"]}],
            }
            for mode in ("off", "on", "on", "off")
        ],
    }
    if change == "v1":
        packet["spec"]["definition"] = "total_observer_control.v1"
    elif change == "fixture":
        packet["evidence_kind"] = "fixture"
    elif change == "output":
        packet["rows"][0]["requests"][0]["output_sha256"] = "wrong"
    elif change == "source":
        packet["spec"]["tool_source_sha256"] = "b" * 64
    elif change == "chronology":
        packet["rows"][0]["after_close_ns"] = 1000
    elif change == "config":
        packet["spec"]["config_sha256"] = "b" * 64
    (control / "packet.json").write_bytes(json_bytes(packet))
    (overhead / "total-control-binding.json").write_bytes(
        json_bytes(
            {
                "definition": "total_observer_binding.v1",
                "path": str(control),
                "manifest_sha256": binding.sha256_file(control / "manifest.json"),
            }
        )
    )
    monkeypatch.setattr(binding, "verify_packet", lambda root: {"hardware_qualified": True})
    monkeypatch.setattr(binding, "read_trial", lambda root: data)
    monkeypatch.setattr(
        binding, "workload_identity", lambda value: object() if change == "workload" else "fixture"
    )
    original_read = binding.read_json

    def read(path):
        if path.name == "collector.json":
            return {
                "collector": "other"
                if change == "collector" and path.parent == target
                else "fixture",
                "sensors": [],
                "interval_ms": 500,
            }
        return original_read(path)

    monkeypatch.setattr(binding, "read_json", read)
    proof = binding.load_total_binding(overhead, target, {"boot_id": "boot", "start_ns": 1000})
    assert not proof["eligible"]
    assert any(reason in r for r in proof["reasons"])


def test_changed_reference_manifest_is_not_trusted(tmp_path):
    control = tmp_path / "control"
    control.mkdir()
    (control / "manifest.json").write_bytes(b"changed")
    (tmp_path / "total-control-binding.json").write_bytes(
        json_bytes(
            {
                "definition": "total_observer_binding.v1",
                "path": str(control),
                "manifest_sha256": "a" * 64,
            }
        )
    )
    with pytest.raises(EvidenceError, match="manifest_mismatch"):
        binding.load_total_binding(tmp_path, tmp_path, {})
