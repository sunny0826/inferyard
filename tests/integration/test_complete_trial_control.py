"""Actual formal journal versus a deferred buffer, using a synthetic endpoint."""

import asyncio
import os
from dataclasses import asdict

import pytest

from inferyard.config.single_plan import compile_single_plan
from inferyard.evidence.storage import EvidenceError, Redactor, json_bytes, read_json
from inferyard.extensions.complete_trial_control import execute_complete, read_trial_plan
from inferyard.extensions.extension_evidence import ExtensionJournal
from inferyard.extensions.independent_guard import POLICY
from inferyard.extensions.trial_control_evidence import verify_control_rows
from inferyard.extensions.workflow import digest
from inferyard.platforms.external_cpu import read_boot_id
from inferyard.provenance import tool_source_hash
from inferyard.runtime.trial_runner import TrialDependencies
from tests.integration.test_runner import scenario as runner_scenario

scenario = runner_scenario


def test_complete_control_uses_real_trial_seals_and_deferred_baselines(
    scenario, tmp_path, monkeypatch
):
    loaded, deps = scenario[0].config, scenario[1]
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    path = tmp_path / "measurement-plan.json"
    path.write_bytes(json_bytes(plan))
    spec = {
        "schema_version": 3,
        "definition": "total_observer_control.v2",
        "case_ids": plan["trials"][0]["case_order"],
        "tolerance_ratio": 0.05,
        "max_wall_seconds": 60,
        "max_guard_gap_ns": 2_000_000_000,
        "tool_source_sha256": tool_source_hash(),
        "config_sha256": digest(config),
        "bundle_sha256": digest(bundle),
        "guardian_policy_sha256": digest(POLICY),
        "trial_plan_path": str(path),
        "trial_plan_sha256": plan["plan_sha256"],
        "trial_id": plan["trials"][0]["trial_id"],
    }
    assert read_trial_plan(spec, config, bundle, digest) == plan
    monkeypatch.setattr("inferyard.runtime.trial_runner.require_review", lambda bundle: None)

    class Guard:
        clock_id = read_boot_id() + ":CLOCK_MONOTONIC"

        def stopped(self):
            return False

    guard = Guard()
    store = ExtensionJournal(tmp_path / "control", Redactor())
    store.snapshot("measurement-plan.json", plan)
    dependencies = TrialDependencies(**asdict(deps))
    with deps.lock() as lock:
        arms = asyncio.run(
            execute_complete(spec, plan, loaded, store, lock, guard, dependencies=dependencies)
        )
    assert [a["mode"] for a in arms] == ["off", "on", "on", "off"]
    assert len(scenario[2]) == 32  # each arm: two probes, three warmups, three cases
    assert all(a["finalized"] and a["measurement_path"] == "run_trial.v1" for a in arms)
    children = [
        {"path": a["trial_path"], "manifest_sha256": a["trial_manifest_sha256"]}
        for a in arms
        if a["mode"] == "on"
    ]
    store.snapshot("child-evidence.json", children)
    samples = [
        {"monotonic_ns": arms[0]["begin_ns"] - 1, "safe": True},
        {"monotonic_ns": arms[-1]["after_close_ns"] + 1, "safe": True},
    ]
    (store.path / "guardian.jsonl").write_bytes(b"".join(json_bytes(s) for s in samples))
    packet = {
        "spec": spec,
        "rows": arms,
        "guard": {"clock_id": guard.clock_id, "pid": os.getpid() + 1, "samples": samples},
    }
    verify_control_rows(store.path, packet)
    baseline_path = store.path / arms[0]["baseline_path"]
    baseline = read_json(baseline_path)
    assert baseline["preservation"] == "memory_during_arm_file_after_measured_close"
    assert "offline_reconstruction" in arms[1]["coverage"]
    baseline["requests"][0]["content"] = "tampered"
    baseline_path.write_bytes(json_bytes(baseline))
    with pytest.raises(EvidenceError, match="raw_requests_mismatch"):
        verify_control_rows(store.path, packet)
    store.close()


def test_baseline_redacts_preserved_responses_before_disk(scenario, tmp_path):
    from inferyard.extensions.trial_control_baseline import BaselineJournal

    config, bundle = scenario[0].config.config.to_dict(), scenario[0].config.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    journal = BaselineJournal(
        tmp_path,
        plan,
        plan["trials"][0]["trial_id"],
        config,
        bundle,
        redactor=Redactor(["private-value"]),
    )
    journal.event("probe", "probe", None, {"content": "private-value"})
    journal.observation("safety", {"message": "private-value"})
    journal.snapshot("snapshot.json", {"message": "private-value"})
    raw = json_bytes(journal.buffer())
    assert b"private-value" not in raw and b"[REDACTED]" in raw


def test_full_control_plan_cannot_substitute_another_workload(scenario, tmp_path):
    config, bundle = scenario[0].config.config.to_dict(), scenario[0].config.bundle.to_dict()
    plan = compile_single_plan(config, bundle)
    path = tmp_path / "plan.json"
    path.write_bytes(json_bytes(plan))
    spec = {
        "trial_plan_path": str(path),
        "trial_plan_sha256": plan["plan_sha256"],
        "trial_id": plan["trials"][0]["trial_id"],
        "case_ids": ["other"],
    }
    with pytest.raises(EvidenceError, match="inputs_mismatch"):
        read_trial_plan(spec, config, bundle, digest)
