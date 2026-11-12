"""Authenticated read of a dedicated context-speed batch, without writing artifacts."""

from inferyard.config.context_speed_profile import classify_verified_inputs
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.errors import EvidenceError
from inferyard.evidence.storage import local_file
from inferyard.runtime.batch_state import history


class NotSpeed(ValueError):
    pass


def read_inputs(root):
    plan, loaded = read_frozen_plan(local_file(root, "frozen-plan/plan.json"))
    profiles = [
        classify_verified_inputs(plan, item.config.to_dict(), item.bundle.to_dict())
        for item in loaded.values()
    ]
    if not any(profiles):
        raise NotSpeed("context_speed_batch_required")
    runs = history(root, plan, loaded, allow_tool_change=True, full_verification=True)
    if len({d["run"]["trial_id"] for d in runs}) != len(runs):
        raise EvidenceError("context_speed_repeated_trial")
    for data in runs:
        if data["run"]["relation"] == "resume" or len(data["requests"]) != 1:
            raise EvidenceError("context_speed_trial_scope_mismatch")
        if data["requests"][0]["execution_state"] == "not_executed":
            raise EvidenceError("context_speed_not_executed_with_run")
    return plan, loaded, runs
