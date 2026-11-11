"""Shared execution counts and completeness for full and projected ledgers."""

from collections import Counter

from inferyard.analysis.aggregate import STATES


def trial_summary(run, requests, stopped, limits, window, duration):
    counts = Counter(r["execution_state"] for r in requests)
    counts = {s: counts[s] for s in STATES}
    counts.update(
        planned=len(requests),
        executed=len(requests) - counts["not_executed"],
        valid_executed=counts["completed"] + counts["failed"],
        budget_exhausted=sum(
            bool(r.get("budget_exhausted"))
            for r in requests
            if r["execution_state"] in ("completed", "failed")
        ),
        budget_exhausted_completed=sum(
            bool(r.get("budget_exhausted")) for r in requests if r["execution_state"] == "completed"
        ),
        budget_exhausted_other_diagnostic=sum(
            bool(r.get("budget_exhausted"))
            for r in requests
            if r["execution_state"] not in ("completed", "failed")
        ),
    )
    scope_complete = (
        stopped == "plan_finished"
        and counts["valid_executed"] == len(requests)
        and not any("truncated" in item for item in limits)
    )
    if window is not None:
        scope_complete &= window["window_completed"] and window.get(
            "probe_coverage_complete", False
        )
    quality_ready = all(r["quality_state"] in ("pass", "fail", "not_applicable") for r in requests)
    evidence_complete = not any(
        "truncated" in item or "manifest_missing" in item for item in limits
    )
    complete = (
        scope_complete
        and quality_ready
        and evidence_complete
        and run["relation"] != "resume"
        and not run["diagnostic"]
        and run["kind"] == "run"
    )
    if window is not None:
        counts["planned"] = None
        counts["request_limit"] = duration["max_requests"]
    return {
        "stop_reason": stopped or "run_stop_record_missing",
        "counts": counts,
        "completeness": "complete" if complete else "incomplete",
        "scope_complete": scope_complete,
        "evidence_complete": evidence_complete,
        **({"duration": window} if window is not None else {}),
    }
