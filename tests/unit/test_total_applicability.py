"""Applicability decisions with a mocked packet verifier, never hardware qualification."""

from copy import deepcopy

import pytest

from inferyard.evidence import total_applicability as binding
from inferyard.evidence.storage import EvidenceError, json_bytes, sha256_file
from inferyard.reporting.comparison_report import comparison_input
from inferyard.runtime.overhead_runner import request_projection
from tests.helpers import fixture_run


def setup_control(tmp_path, monkeypatch):
    target = fixture_run(tmp_path / "target", states=["completed"] * 3)
    data, reference = comparison_input(target)
    data["environment_start"]["cpu_policies"] = [{"policy": "fixture"}]
    data["summary"]["measurement_context"]["environment_qualification"]["eligible"] = True
    control = deepcopy(data)
    metadata = {"environment_start": control["environment_start"], "identity": control["identity"]}
    root = tmp_path / "control"
    root.mkdir()
    (root / "manifest.json").write_bytes(b"mocked verifier manifest")
    overhead = tmp_path / "overhead"
    overhead.mkdir()
    (overhead / "total-control-binding.json").write_bytes(
        json_bytes(
            {
                "definition": "total_observer_binding.v1",
                "path": str(root),
                "manifest_sha256": sha256_file(root / "manifest.json"),
            }
        )
    )
    rows = [{"output_sha256": request_projection(r)["output_identity"]} for r in data["requests"]]
    packet = {
        "evidence_kind": "live",
        "spec": {
            "definition": "total_observer_control.v2",
            "case_ids": data["selection"]["case_ids"],
            "tolerance_ratio": 0.05,
        },
        "guard": {"clock_id": "boot:CLOCK_MONOTONIC"},
        "rows": [
            {"mode": mode, "after_close_ns": 10, "trial_path": str(i), "requests": deepcopy(rows)}
            for i, mode in enumerate(("off", "on", "on", "off"))
        ],
    }
    calls = []

    def verify(path, *, verified_trials):
        calls.append(path)
        verified_trials.update({str(i): (control, metadata) for i in (1, 2)})
        return {"hardware_qualified": True}

    monkeypatch.setattr(binding, "verify_packet", verify)
    original = binding.read_json
    monkeypatch.setattr(
        binding,
        "read_json",
        lambda path: (
            packet
            if path.name == "packet.json"
            else {"collector": "fixture", "sensors": [], "interval_ms": 500}
            if path.name == "collector.json"
            else original(path)
        ),
    )
    monkeypatch.setattr(
        binding, "clock_span", lambda path: {"boot_id": "boot", "start_ns": 20, "end_ns": 30}
    )
    return overhead, target, data, reference, control, packet, calls


def test_total_only_reuses_verified_arms_and_ignores_execution_locators(tmp_path, monkeypatch):
    overhead, target, data, ref, control, _, calls = setup_control(tmp_path, monkeypatch)
    data["config"]["endpoint"].update(
        server_pid=456, process_start_ticks=999, url="http://127.0.0.1:9999"
    )
    data["config"]["model"]["local_path"] = "/other/location.gguf"
    data["config"]["model"]["template_path"] = "/other/template.jinja"
    data["config"]["engine"]["binary_path"] = "/other/server"
    data["config"]["output"]["root"] = "/other/results"
    result = binding.load_performance_evidence_v3(overhead, target, data=data, reference=ref)
    assert result["eligible"], result["reasons"]
    assert len(calls) == 1
    assert result["incremental_diagnostic"] == {"status": "not_supplied"}
    assert data["run"]["implementation_identity"] == control["run"]["implementation_identity"]


def test_explicit_control_root_mapping_keeps_binding_bytes_and_checks_manifest(
    tmp_path, monkeypatch
):
    overhead, target, data, ref, _, _, calls = setup_control(tmp_path, monkeypatch)
    before = (overhead / "total-control-binding.json").read_bytes()
    old, moved = tmp_path / "control", tmp_path / "moved-control"
    old.rename(moved)
    options = dict(data=data, reference=ref, source_roots=((old, moved),))
    result = binding.load_performance_evidence_v3(overhead, target, **options)
    assert result["eligible"]  # Mocked packet semantics only; not hardware qualification.
    assert calls == [moved]
    assert (overhead / "total-control-binding.json").read_bytes() == before
    (moved / "manifest.json").write_bytes(b"changed source manifest")
    with pytest.raises(EvidenceError, match="manifest_mismatch"):
        binding.load_performance_evidence_v3(overhead, target, **options)


def test_incremental_tolerance_or_failure_does_not_replace_total_estimate(tmp_path, monkeypatch):
    overhead, target, data, ref, _, _, _ = setup_control(tmp_path, monkeypatch)
    (overhead / "protocol.json").write_text("{}")
    (overhead / "trials.json").write_text("[]")
    monkeypatch.setattr(
        binding,
        "read_overhead",
        lambda root, **kw: {
            "passed": False,
            "tolerance_ratio": 0.2,
            "target_binding": {"applicable": False},
            "environment_binding": {"eligible": False},
        },
    )
    result = binding.load_performance_evidence_v3(overhead, target, data=data, reference=ref)
    assert result["eligible"] and result["tolerance_ratio"] == 0.05
    assert result["incremental_diagnostic"]["status"] == "limited"
    assert result["assessment"] == {}


def test_incompatible_targets_and_unknown_scope_refused(tmp_path, monkeypatch):
    overhead, target, original, ref, _, packet, _ = setup_control(tmp_path, monkeypatch)
    cases = ("output", "measurement", "loading", "collector", "boot", "environment")
    for change in cases:
        data = deepcopy(original)
        if change == "output":
            data["requests"][0]["content"] = "different observed output"
        elif change == "measurement":
            data["run"].pop("implementation_identity")
        elif change == "loading":
            data["config"]["conditions"]["threads"] += 1
        elif change == "collector":
            data["config"]["telemetry"]["interval_ms"] += 100
        elif change == "boot":
            packet["guard"]["clock_id"] = "other:CLOCK_MONOTONIC"
        else:
            packet["guard"]["clock_id"] = "boot:CLOCK_MONOTONIC"
            data["environment_start"]["ac_online"] = None
        result = binding.load_performance_evidence_v3(overhead, target, data=data, reference=ref)
        assert not result["eligible"], change
    (overhead / "total-control-binding.json").write_bytes(
        json_bytes(
            {
                "definition": "total_observer_binding.v1",
                "path": str(tmp_path / "control"),
                "manifest_sha256": "a" * 64,
            }
        )
    )
    with pytest.raises(EvidenceError, match="manifest_mismatch"):
        binding.load_performance_evidence_v3(overhead, target, data=original, reference=ref)
