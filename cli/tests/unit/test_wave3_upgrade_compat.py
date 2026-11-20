"""Upgrade compatibility and version-based consistency tests for Wave 3."""

import hashlib
from copy import deepcopy
from pathlib import Path

import pytest

from inferyard import SCHEMA_VERSION
from inferyard.analysis.comparison import compare_trials
from inferyard.analysis.ledger import read_trial
from inferyard.analysis.repetition_analysis import make_analysis
from inferyard.analysis.scoring_revision import VERSION as REVISION_VERSION
from inferyard.analysis.scoring_revision import scorer_hash as revision_scorer_hash
from inferyard.contracts.batch_envelope import validate_batch_envelope
from inferyard.contracts.validation import json_bytes, validate_document
from inferyard.errors import ContractError, EvidenceError
from inferyard.evidence.storage import atomic_bytes, read_json
from inferyard.reporting.comparison_report import (
    build_comparison,
    verify_comparison,
)
from inferyard.reporting.rescore import verify_rescore, write_rescore
from inferyard.reporting.total_applicability import domain_reasons
from inferyard.runtime.overhead_binding import bind_target, workload_identity
from tests.helpers import fixture_run


def test_legacy_run_and_batch_with_implementation_identity_validate(tmp_path: Path) -> None:
    root = fixture_run(tmp_path / "run", states=["completed"] * 3, comparison_mode="model")
    run = read_json(root / "run.json")
    legacy_both = deepcopy(run)
    legacy_both["implementation_identity"] = {
        "schema_version": SCHEMA_VERSION,
        "measurement": {"sha256": "a" * 64, "files": [], "dependencies": []},
        "scoring": {"sha256": "b" * 64, "files": [], "dependencies": []},
    }
    validate_document("run", legacy_both)
    legacy_only = deepcopy(legacy_both)
    legacy_only.pop("tool_source_sha256")
    validate_document("run", legacy_only)

    batch_meta = {
        "schema_version": SCHEMA_VERSION,
        "plan_sha256": "c" * 64,
        "diagnostic": False,
        "tool_source_sha256": "d" * 64,
        "implementation_identity": legacy_both["implementation_identity"],
    }
    validate_batch_envelope(batch_meta, "c" * 64)
    batch_only = deepcopy(batch_meta)
    batch_only.pop("tool_source_sha256")
    validate_batch_envelope(batch_only, "c" * 64)


def test_legacy_format_v5_and_new_format_v6_comparisons_both_verify(tmp_path: Path) -> None:
    left = fixture_run(tmp_path / "left", states=["completed"] * 3, comparison_mode="model")
    right = fixture_run(tmp_path / "right", states=["completed"] * 3, comparison_mode="model")

    v5_dir = tmp_path / "cmp-v5"
    v5_dir.mkdir()
    v5 = build_comparison(left, right, format_version=5)
    assert v5["definition"] == "phase2.v3"
    assert any(c["field"] == "run.tool_source_sha256" for c in v5["conditions"])
    atomic_bytes(v5_dir / "comparison.json", json_bytes(v5))
    assert verify_comparison(v5_dir)["verified"]

    v6_dir = tmp_path / "cmp-v6"
    v6_dir.mkdir()
    v6 = build_comparison(left, right, format_version=6)
    assert v6["definition"] == "phase2.v4"
    assert all(c["field"] != "run.tool_source_sha256" for c in v6["conditions"])
    atomic_bytes(v6_dir / "comparison.json", json_bytes(v6))
    assert verify_comparison(v6_dir)["verified"]


def test_tool_version_enforced_in_observed_comparison_and_total_applicability(
    tmp_path: Path,
) -> None:
    root = fixture_run(tmp_path / "base", states=["completed"] * 3, comparison_mode="model")
    left = read_trial(root)
    right = deepcopy(left)
    right["run"]["tool_version"] = "9.9.9"

    result = compare_trials(left, right)
    assert result["definition"] == "phase2.v4"
    observed = result["observed_differences"]
    assert "tool_version_mismatch" in observed["reasons"]
    assert observed["completion_rate"]["difference"] is None

    reasons = domain_reasons(left, right, {})
    assert "total_control_tool_version_unknown_or_different" in reasons


def test_rescore_verifies_historical_scorer_hash_and_revision_hash_is_version_based(
    tmp_path: Path,
) -> None:
    assert revision_scorer_hash() == hashlib.sha256(
        f"inferyard.scorer:{REVISION_VERSION}".encode("utf-8")
    ).hexdigest()

    root = fixture_run(tmp_path / "run", states=["completed"] * 3, comparison_mode="model")
    out = tmp_path / "rescore"
    write_rescore(root, out, scorer_id="phase2.v2", reason="upgrade verify")

    # Simulate a legacy rescore saved with a pre-Wave-3 source-byte scorer_sha256
    legacy_hash = "e" * 64
    record = read_json(out / "rescore.json")
    analysis = read_json(out / "analysis.json")
    parent = read_json(out / "parent-analysis.json")
    record["scorer_sha256"] = legacy_hash
    for change in record["changes"]:
        if change["new_score"] is not None:
            change["new_score"]["scorer_sha256"] = legacy_hash
    record.pop("analysis_id")
    aid = "rescore-" + hashlib.sha256(json_bytes(record)).hexdigest()[:32]
    record["analysis_id"] = aid
    analysis["analysis_id"] = aid
    analysis["scorer_sha256"] = legacy_hash
    evidence_hashes = {
        "rescore.json": hashlib.sha256(json_bytes(record)).hexdigest(),
        "parent-analysis.json": hashlib.sha256(json_bytes(parent)).hexdigest(),
    }
    for metric in analysis["metrics"]:
        for ref in metric["evidence_refs"]:
            if ref["path"] in evidence_hashes:
                ref["sha256"] = evidence_hashes[ref["path"]]
    atomic_bytes(out / "rescore.json", json_bytes(record), overwrite=True)
    atomic_bytes(out / "analysis.json", json_bytes(analysis), overwrite=True)

    assert verify_rescore(out)["verified"]

    # Tampering a score must still fail verification
    tampered = read_json(out / "rescore.json")
    tampered["reason"] = "tampered"
    atomic_bytes(out / "rescore.json", json_bytes(tampered), overwrite=True)
    with pytest.raises(EvidenceError):
        verify_rescore(out)


def test_repetition_analysis_uses_explicit_scoring_definition_version(tmp_path: Path) -> None:
    left_path = fixture_run(tmp_path / "r1", states=["completed"] * 3, comparison_mode="model")
    right_path = fixture_run(tmp_path / "r2", states=["completed"] * 3, comparison_mode="model")
    r1 = read_trial(left_path)
    r2 = read_trial(right_path)
    r1["path"], r2["path"] = left_path, right_path
    r1["selection"]["scorer_sha256"] = "1" * 64
    r2["selection"]["scorer_sha256"] = "2" * 64
    group = {
        "workload_id": r1["plan"]["experiment"]["workloads"][0]["workload_id"],
        "run_ids": [r1["run"]["run_id"], r2["run"]["run_id"]],
        "expected_repeats": 2,
        "limitations": [],
        "cases": [{"case_id": "case-1", "paired_latency_difference_ms": [0.0, 2.0]}],
        "S03": {"value": 1.0, "numerator": 3, "denominator": 3, "reason": None},
        "S04": {
            "trial_medians_ms": [10.0, 12.0],
            "range_ms": 2.0,
            "iqr_ms": 1.0,
            "reason": None,
        },
    }
    analysis = make_analysis(
        group,
        [r1, r2],
        r1["run"]["definition_versions"],
        [{"path": "repeat.json", "sha256": "a" * 64}],
    )
    assert analysis is not None

    r2["run"]["definition_versions"]["scoring"] = "phase2.v2"
    with pytest.raises(EvidenceError, match="repeat_scoring_definition_mismatch"):
        make_analysis(
            group,
            [r1, r2],
            r1["run"]["definition_versions"],
            [{"path": "repeat.json", "sha256": "a" * 64}],
        )


def test_overhead_bind_target_uses_tool_version_not_source_hash(tmp_path: Path) -> None:
    root = fixture_run(tmp_path / "target", states=["completed"] * 3, comparison_mode="model")
    data = read_trial(root)
    config_sha = hashlib.sha256(json_bytes(data["config"])).hexdigest()
    bundle_sha = hashlib.sha256(json_bytes(data["bundle"])).hexdigest()
    protocol = {
        "kind": "collector_overhead_abba.v1",
        "protocol_sha256": "a" * 64,
        "config_sha256": config_sha,
        "bundle_sha256": bundle_sha,
        "tool_source_sha256": "0" * 64,
        "case_ids": [r["case_id"] for r in data["requests"]],
        "resource_collector": "linux-resource.v2",
    }
    from inferyard.runtime.overhead_runner import request_projection

    arm_requests = [request_projection(r) for r in data["requests"]]
    bound = bind_target(
        root,
        data,
        protocol,
        workload_identity(data),
        arm_requests,
        {"passed": True},
        {"collector": "linux-resource.v2"},
        arm_tool_version=data["run"]["tool_version"],
    )
    assert bound["applicable"]
    assert "target_tool_mismatch" not in bound["reasons"]

    mismatched = bind_target(
        root,
        data,
        protocol,
        workload_identity(data),
        arm_requests,
        {"passed": True},
        {"collector": "linux-resource.v2"},
        arm_tool_version="0.0.1",
    )
    assert not mismatched["applicable"]
    assert "target_tool_mismatch" in mismatched["reasons"]

    # Legacy v5 replay retains source-hash binding check so v5 reports recompute identically
    legacy_v5 = bind_target(
        root,
        data,
        protocol,
        workload_identity(data),
        arm_requests,
        {"passed": True},
        {"collector": "linux-resource.v2"},
        arm_tool_version=data["run"]["tool_version"],
        legacy_source_binding=True,
    )
    assert not legacy_v5["applicable"]
    assert "target_tool_mismatch" in legacy_v5["reasons"]


def _valid_legacy_identity(file_sha: str) -> dict:
    roles = {}
    for role in ("measurement", "scoring", "presentation"):
        body = {
            "files": [{"path": f"{role}.py", "sha256": file_sha}],
            "dependencies": [{"name": "jsonschema", "version": "4.26.0", "reason": None}],
        }
        roles[role] = {**body, "sha256": hashlib.sha256(json_bytes(body)).hexdigest()}
    return {"definition": "implementation-identity.v1", **roles}


def test_legacy_v5_comparison_and_total_applicability_validate_archived_role_digests(
    tmp_path: Path,
) -> None:
    root = fixture_run(tmp_path / "base", states=["completed"] * 3, comparison_mode="model")
    left = read_trial(root)
    right = deepcopy(left)
    identity = _valid_legacy_identity("a" * 64)
    left["run"]["implementation_identity"] = deepcopy(identity)
    right["run"]["implementation_identity"] = deepcopy(identity)

    valid_v5 = compare_trials(left, right, definition="phase2.v3")
    assert (
        next(c for c in valid_v5["conditions"] if c["field"] == "run.implementation_identity.measurement")[
            "status"
        ]
        == "same"
    )
    assert "total_control_measurement_identity_unknown_or_different" not in domain_reasons(
        left, right, {}, legacy_source_binding=True
    )

    # Tampering inner measurement.files while keeping outer sha256 must fail validation
    right["run"]["implementation_identity"]["measurement"]["files"][0]["sha256"] = "b" * 64
    with pytest.raises(ContractError, match="content digest mismatch"):
        compare_trials(left, right, definition="phase2.v3")
    with pytest.raises(ContractError, match="content digest mismatch"):
        domain_reasons(left, right, {}, legacy_source_binding=True)
