"""Replay the frozen first-failure stopping invariant from request evidence."""

from inferyard.evidence.storage import EvidenceError


def validate_capacity_stop(experiment, requests, stopped):
    if experiment.get("capacity_stop") != "first_failed_request":
        return
    failures = [i for i, r in enumerate(requests) if r["execution_state"] == "failed"]
    if failures:
        if stopped == "plan_finished" or any(
            r["request_id"] is not None for r in requests[failures[0] + 1 :]
        ):
            raise EvidenceError("capacity_stop_protocol_violated")
    elif stopped == "capacity_scan_failed_request":
        raise EvidenceError("capacity_stop_without_failed_request")
