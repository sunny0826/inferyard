"""Canonical plan identity and bounded, explicitly scoped budget arithmetic."""

import hashlib
import json


def plan_hash(plan):
    payload = {key: value for key, value in plan.items() if key != "plan_sha256"}
    return hashlib.sha256(
        json.dumps(
            payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False
        ).encode()
    ).hexdigest()


def workload_budget(workload):
    protocol = workload["protocol"]
    limit = len(protocol["case_ids"]) if protocol["kind"] == "fixed" else protocol["max_requests"]
    request_seconds = limit * workload["timeout_seconds"]
    active_seconds = (
        request_seconds
        if protocol["kind"] == "fixed"
        else protocol["duration_seconds"] + protocol["drain_timeout_seconds"]
    )
    return limit, request_seconds, active_seconds + workload["overhead_budget_seconds"]
