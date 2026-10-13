"""Measure the actual fixed-trial journal lifecycle, not a lookalike extension log."""

from dataclasses import replace
from pathlib import Path

from inferyard.contracts.validation import validate_document
from inferyard.evidence.journal import trial_for
from inferyard.evidence.storage import EvidenceError, read_json, sha256_file
from inferyard.extensions.total_observer_control import run_total
from inferyard.extensions.trial_control_baseline import BaselineJournal, CommonGuardSafety
from inferyard.runtime.overhead_runner import ResourceOffSampler, request_projection
from inferyard.runtime.trial_runner import TrialDependencies, run_trial


def read_trial_plan(spec, config, bundle, digest):
    plan = read_json(Path(spec["trial_plan_path"]))
    validate_document("plan", plan)
    if plan["plan_sha256"] != spec["trial_plan_sha256"]:
        raise EvidenceError("total_control_trial_plan_hash_mismatch")
    trial = trial_for(plan, spec["trial_id"])
    workload = next(
        w for w in plan["experiment"]["workloads"] if w["workload_id"] == trial["workload_id"]
    )
    if (
        workload["protocol"]["kind"] != "fixed"
        or workload.get("output_mode")
        or workload.get("cache_protocol")
        or trial["case_order"] != spec["case_ids"]
        or workload["config"]["sha256"] != digest(config)
        or workload["bundle"]["sha256"] != digest(bundle)
    ):
        raise EvidenceError("total_control_trial_inputs_mismatch")
    return plan


async def execute_complete(
    spec, plan, loaded, parent_store, lock, guard, *, dependencies=None, identity_context=None
):
    from inferyard.analysis.scoring import ScoringContext
    from inferyard.implementation_identity import IdentityContext

    identity_context = identity_context or IdentityContext()
    scoring_context = ScoringContext()
    deps = dependencies or TrialDependencies()
    pending = []

    class Arm:
        def __init__(self, mode):
            self.mode, self.baseline, self.trial_path = mode, None, None
            self.index = len(pending) + 1
            pending.append(self)

        async def open(self):
            pass

        async def close(self):
            pass

        def deferred_evidence(self):
            identity = {
                "measurement_path": "run_trial.v1",
                "trial_plan_sha256": plan["plan_sha256"],
            }
            if self.mode == "off" and self.baseline is not None:
                filename = f"baseline-{self.index}.json"
                parent_store.snapshot(filename, self.baseline.buffer())
                identity["baseline_path"] = filename
            elif self.trial_path is not None:
                identity["trial_path"] = str(self.trial_path.relative_to(parent_store.path))
                identity["trial_manifest_sha256"] = sha256_file(self.trial_path / "manifest.json")
            return identity

    async def workload(arm):
        if guard.stopped():
            raise EvidenceError("total_control_common_guard_stopped")

        def journal(*args, **kwargs):
            arm.baseline = BaselineJournal(parent_store.path, *args[1:], **kwargs)
            return arm.baseline

        local = (
            deps
            if arm.mode == "on"
            else replace(
                deps,
                journal=journal,
                readout=lambda store: store.data(),
                sampler=ResourceOffSampler,
                safety=CommonGuardSafety,
            )
        )
        code, data, path = await run_trial(
            plan,
            spec["trial_id"],
            loaded,
            parent_store.path / "observer-arms",
            dependencies=local,
            host_lock=lock,
            wall_budget_seconds=spec["max_wall_seconds"],
            source_identity=identity_context.source,
            implementation_identity=identity_context.value,
            scoring_context=scoring_context,
        )
        if arm.mode == "on":
            arm.trial_path = path
        if code != 0 or guard.stopped():
            raise EvidenceError("total_control_formal_trial_incomplete")
        rows = [request_projection(row) for row in data["requests"]]
        if any(r["execution_state"] != "completed" or r["output_identity"] is None for r in rows):
            raise EvidenceError("total_control_formal_output_incomplete")
        return [
            {
                "case_id": r["case_id"],
                "state": r["execution_state"],
                "duration_ns": r["duration_ns"],
                "output_sha256": r["output_identity"],
            }
            for r in rows
        ]

    def checkpoint(arms):
        parent_store.event("control_checkpoint", "formal", None, {"arms": arms})

    return await run_total(spec, workload, Arm, guard, arm_sink=checkpoint)
