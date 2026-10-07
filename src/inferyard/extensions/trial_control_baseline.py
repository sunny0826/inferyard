"""In-memory response preservation for a frozen, guarded observation-off trial.

Only the total-control driver uses this journal. It is never a standalone
benchmark run, and its buffer is saved after the measured arm has closed.
HostLock/dirty, actual request execution, scoring and verified drain stay intact.
"""

import uuid
from copy import deepcopy

from inferyard.analysis.scoring import ScoringContext
from inferyard.evidence.journal import trial_for
from inferyard.platforms.external_cpu import read_boot_id


class BaselineJournal:
    def __init__(self, root, plan, trial_id, config, bundle, *, redactor, **kwargs):
        trial = trial_for(plan, trial_id)
        self.path, self.run_id = root, "baseline-" + uuid.uuid4().hex
        self.kind, self.clock_id = "run", read_boot_id() + ":CLOCK_MONOTONIC"
        self.selected = trial["case_order"]
        self.scoring_context = kwargs.get("scoring_context") or ScoringContext()
        self.scorer_sha256, self.redactor = self.scoring_context.identity(), redactor
        self.duration_protocol = self.cache_protocol = None
        self.strict_output, self.sealed = False, False
        self.bundle, self.config = bundle, config
        self.events, self.snapshots, self.observations = [], {}, []
        self.snapshot("plan.json", plan)
        self.snapshot("config.frozen.json", config)
        self.snapshot("bundle.json", bundle)

    def snapshot(self, name, data):
        self.snapshots[name] = deepcopy(self.redactor.clean(data))

    def event(self, kind, phase, request_id, data, *, monotonic_ns=None):
        self.events.append(
            {
                "event_type": kind,
                "phase": phase,
                "request_id": request_id,
                "data": deepcopy(self.redactor.clean(data)),
                "monotonic_ns": monotonic_ns,
            }
        )

    def observation(self, name, data):
        self.observations.append({"path": name, "data": deepcopy(self.redactor.clean(data))})

    def seal(self):
        self.sealed = True

    def close(self):
        pass

    def data(self):
        starts = {
            e["request_id"]: e["data"]
            for e in self.events
            if e["event_type"] == "request_started" and e["phase"] == "formal"
        }
        requests = [
            {**e["data"], "case_id": starts[e["request_id"]]["case_id"]}
            for e in self.events
            if e["event_type"] == "request_finished" and e["phase"] == "formal"
        ]
        return {"requests": requests}

    def buffer(self):
        return {
            "definition": "trial_control_baseline.v1",
            "run_id": self.run_id,
            "clock_id": self.clock_id,
            "snapshots": self.snapshots,
            "events": self.events,
            "observations": self.observations,
            "requests": self.data()["requests"],
            "preservation": "memory_during_arm_file_after_measured_close",
        }


class CommonGuardSafety:
    """Independent common guard substitutes for in-process observation in off arms."""

    def __init__(self, policy, config, environment):
        self.policy, self.last = policy, None

    def check(self, *, periodic=False):
        pass

    def metadata(self):
        return {"policy": self.policy, "scope": "separately_bound_common_independent_guard"}
