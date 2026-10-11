"""Standalone read-only memory diagnostic; never enters benchmark execution."""

import os
import platform
from datetime import UTC, datetime

from inferyard.application.types import CommandResult
from inferyard.contracts.memory_check import validate_memory_check
from inferyard.contracts.validation import ContractError
from inferyard.evidence.storage import atomic_bytes, json_bytes

# Defaults are frozen after the MC-1a native observation; see the diagnostic contract.
DEADLINE_SECONDS = 5
MAX_PROCESSES = 4096
LIMITATIONS = (
    "rss_sum_is_not_releasable_memory",
    "application_membership_may_be_incomplete",
    "embedded_or_unrecognized_agents_cannot_be_identified",
    "review_requires_user_check_of_work_tasks_and_connections",
    "snapshot_is_not_atomic_or_authorization_to_close",
)


def inspect_memory(protect_pids=(), *, collector=None, uid=None):
    if any(type(pid) is not int or pid <= 0 for pid in protect_pids):
        raise ContractError("protect_pids", "PID must be a positive integer")
    windows = platform.system() == "Windows" and uid is None
    if windows:
        from inferyard.application.memory_windows_decisions import missing as is_missing
        from inferyard.application.memory_windows_decisions import project
        from inferyard.platforms.memory_windows import collect
    else:
        from inferyard.application.memory_decisions import project
        from inferyard.platforms.memory_macos import collect

    snapshot = (collector or collect)(
        deadline_seconds=DEADLINE_SECONDS, max_processes=MAX_PROCESSES
    )
    if windows:
        processes, groups, requests = project(snapshot, protect_pids)
        missing = sum(is_missing(row) for row in processes)
    else:
        processes, groups, requests = project(
            snapshot, protect_pids, os.getuid() if uid is None else uid
        )
        missing = sum(
            row["missing_reason"] is not None
            or row.get("parent_identity_missing_reason") is not None
            for row in processes
        )
    unobserved = snapshot["total_pids"] - len(processes)
    complete = (
        not missing
        and not unobserved
        and snapshot["truncation_reason"] is None
        and all(r["status"] == "verified" for r in requests)
        and snapshot["memory_total"]["value"] is not None
        and snapshot["memory_available"]["value"] is not None
    )
    report = {
        "schema_version": 3,
        "kind": "memory_check.windows.v1" if windows else "memory_check.v1",
        "platform": "Windows" if windows else "Darwin",
        "captured_at": datetime.now(UTC).isoformat(),
        "collection_ns": snapshot["collection_ns"],
        "memory_total": snapshot["memory_total"],
        "memory_available": snapshot["memory_available"],
        "coverage": {
            "total_pids": snapshot["total_pids"],
            "observed": len(processes),
            "missing": missing,
            "unobserved": unobserved,
            "complete": complete,
            "truncation_reason": snapshot["truncation_reason"],
        },
        "budget": {"deadline_seconds": DEADLINE_SECONDS, "max_processes": MAX_PROCESSES},
        "protected_requests": requests,
        "processes": processes,
        "groups": groups,
        "limitations": list(LIMITATIONS)
        + (
            [
                "private_commit_is_not_resident_or_reclaimable_memory",
                "executable_groups_do_not_prove_application_ownership",
                "windows_unknown_groups_never_recommend_termination",
            ]
            if windows
            else []
        ),
    }
    validate_memory_check(report)
    return report


def execute(request):
    if platform.system() not in ("Darwin", "Windows"):
        return 2, CommandResult(request.command, "blocked", limitations=("unsupported_platform",))
    report = inspect_memory(request.protect_pids or ())
    if request.out:
        request.out.mkdir(parents=True, exist_ok=False)
        atomic_bytes(request.out / "memory-check.json", json_bytes(report))
    complete = report["coverage"]["complete"]
    return (0 if complete else 3), CommandResult(
        request.command,
        "inspected" if complete else "partial",
        "complete" if complete else "incomplete",
        evidence_dir=str(request.out) if request.out else None,
        limitations=tuple(report["limitations"]),
        details=report,
    )
