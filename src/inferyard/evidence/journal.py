"""Append-only trial evidence; never reopen or mutate a prior run."""

import hashlib
import os
import time
from datetime import UTC, datetime

from inferyard import SCHEMA_VERSION, __version__
from inferyard.analysis.scoring import ScoringContext
from inferyard.config.plan_inputs import validate_workload_inputs
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import (
    EvidenceError,
    EvidenceStore,
    fsync_directory,
    json_bytes,
)
from inferyard.platforms.platform_io import open_nofollow
from inferyard.provenance import tool_source_hash


def trial_for(plan, trial_id):
    validate_document("plan", plan)
    for trial in plan["trials"]:
        if trial["trial_id"] == trial_id:
            return trial
    raise EvidenceError("trial_not_in_plan")


class TrialJournal(EvidenceStore):
    def observation(self, name, value):
        if name in ("external-cpu.jsonl", "request-environment.jsonl", "safety-checks.jsonl"):
            if self.sealed:
                raise EvidenceError("sealed_run")
            if name not in self._logs:
                fd = open_nofollow(self.path / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL)
                self._logs[name] = os.fdopen(fd, "wb")
                self._pending_sync.add(name)
                fsync_directory(self.path)
            self._append(name, value)
        else:
            super().observation(name, value)

    def __init__(
        self,
        root,
        plan,
        trial_id,
        config,
        bundle,
        *,
        redactor=None,
        parent=None,
        resume_case_ids=(),
        diagnostic=False,
        run_id=None,
        kind="run",
        execution_mode="experiment",
        rerun_parent=None,
        scoring_context=None,
        source_identity=None,
        implementation_identity=None,
    ):
        if source_identity is None and implementation_identity is None:
            from inferyard.implementation_identity import IdentityContext

            identity_context = IdentityContext(source_hash=tool_source_hash)
            source_identity = identity_context.source
            implementation_identity = identity_context.value
        if rerun_parent and (parent or resume_case_ids):
            raise EvidenceError("rerun_parent_conflicts_with_trial_parent")
        trial = trial_for(plan, trial_id)
        workload = next(
            w for w in plan["experiment"]["workloads"] if w["workload_id"] == trial["workload_id"]
        )
        self.duration_protocol = (
            workload["protocol"] if workload["protocol"]["kind"] == "duration" else None
        )
        self.strict_output = workload.get("output_mode") == "strict_fixed_length"
        self.cache_protocol = workload.get("cache_protocol")
        if self.duration_protocol and resume_case_ids:
            raise EvidenceError("duration_resume_requires_new_window")
        validate_document("config", config)
        validate_document("bundle", bundle)
        validate_workload_inputs(workload, config, bundle)
        selected = list(resume_case_ids) if resume_case_ids else list(trial["case_order"])
        if not selected or len(set(selected)) != len(selected):
            raise EvidenceError("invalid_trial_selection")
        if selected != [cid for cid in trial["case_order"] if cid in set(selected)]:
            raise EvidenceError("selection_changes_frozen_order")
        if not set(selected).issubset(c["case_id"] for c in bundle["cases"]):
            raise EvidenceError("selection_case_absent")
        if resume_case_ids and not parent:
            raise EvidenceError("resume_requires_parent_evidence")
        if parent and parent["run"]["plan_sha256"] != plan["plan_sha256"]:
            raise EvidenceError("parent_plan_mismatch")
        if resume_case_ids:
            if parent["run"]["trial_id"] != trial_id:
                raise EvidenceError("resume_trial_mismatch")
            available = [
                r["case_id"] for r in parent["requests"] if r["execution_state"] == "not_executed"
            ]
            if selected != available:
                raise EvidenceError("resume_must_select_all_and_only_unexecuted")
        elif parent:
            previous = trial_for(plan, parent["run"]["trial_id"])
            if (
                previous["workload_id"] != trial["workload_id"]
                or previous["repeat_index"] + 1 != trial["repeat_index"]
            ):
                raise EvidenceError("repeat_parent_mismatch")
        if redactor and (redactor.clean(config) != config or redactor.clean(bundle) != bundle):
            raise EvidenceError("sensitive_frozen_input")
        self.scoring_context = scoring_context or ScoringContext()
        self.experiment_id = plan["experiment"]["experiment_id"]
        self.trial_id = trial_id
        super().__init__(root, redactor, run_id=run_id)
        ancestor = parent or rerun_parent
        self.kind, self.execution_mode = kind, execution_mode
        run = dict(
            kind=kind,
            execution_mode=execution_mode,
            origin="measured",
            schema_version=SCHEMA_VERSION,
            run_id=self.run_id,
            experiment_id=self.experiment_id,
            trial_id=trial_id,
            plan_sha256=plan["plan_sha256"],
            parent_run_id=ancestor["run"]["run_id"] if ancestor else None,
            relation="rerun"
            if rerun_parent
            else "resume"
            if resume_case_ids
            else "repeat"
            if parent
            else "initial",
            tool_version=__version__,
            tool_source_sha256=source_identity or tool_source_hash(),
            definition_versions=plan["experiment"]["definition_versions"],
            resumed_case_ids=list(resume_case_ids),
            diagnostic=diagnostic,
        )
        if implementation_identity is not None:
            run["implementation_identity"] = implementation_identity
        config_bytes, bundle_bytes = json_bytes(config), json_bytes(bundle)
        selection = dict(
            schema_version=SCHEMA_VERSION,
            run_id=self.run_id,
            trial_id=trial_id,
            case_ids=selected,
            scorer_sha256=self.scoring_context.identity(),
            config_sha256=hashlib.sha256(config_bytes).hexdigest(),
            bundle_sha256=hashlib.sha256(bundle_bytes).hexdigest(),
            parent_events_sha256=ancestor["events_sha256"] if ancestor else None,
        )
        try:
            validate_document("run", run)
            validate_document("selection", selection)
            for name, value in [
                ("run", run),
                ("plan", plan),
                ("selection", selection),
                ("config.frozen", config),
                ("bundle", bundle),
            ]:
                self.snapshot(name + ".json", value)
        except BaseException:
            self.close()
            raise
        self.selected = selected
        self.scorer_sha256 = selection["scorer_sha256"]

    def event(self, event_type, phase, request_id, data, *, monotonic_ns=None):
        seq = self._seq["events.jsonl"] + 1
        event = dict(
            schema_version=SCHEMA_VERSION,
            run_id=self.run_id,
            experiment_id=self.experiment_id,
            trial_id=self.trial_id,
            seq=seq,
            phase=phase,
            request_id=request_id,
            monotonic_ns=time.monotonic_ns() if monotonic_ns is None else monotonic_ns,
            clock_id=self.clock_id,
            utc=datetime.now(UTC).isoformat(),
            event_type=event_type,
            data=data,
        )
        validate_document("event", event)
        self._append(
            "events.jsonl",
            event,
            sync=event_type in ("request_started", "request_finished", "score", "run_stopped"),
        )
        self._seq["events.jsonl"] = seq
        return event

    def sample(self, sample):
        seq = self._seq["memory.jsonl"] + 1
        value = {
            **sample,
            "schema_version": SCHEMA_VERSION,
            "run_id": self.run_id,
            "experiment_id": self.experiment_id,
            "trial_id": self.trial_id,
            "seq": seq,
            "clock_id": self.clock_id,
            "utc": datetime.now(UTC).isoformat(),
        }
        validate_document("sample", value)
        self._append("memory.jsonl", value)
        self._seq["memory.jsonl"] = seq

    def seal(self, extra=None):
        super().seal(
            {
                **(extra or {}),
                "experiment_id": self.experiment_id,
                "trial_id": self.trial_id,
                "kind": self.kind,
                "execution_mode": self.execution_mode,
                "origin": "measured",
            }
        )
