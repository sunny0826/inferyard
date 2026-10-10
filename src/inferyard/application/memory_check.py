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
    from inferyard.application.memory_decisions import project
    from inferyard.platforms.memory_macos import collect

    snapshot = (collector or collect)(
        deadline_seconds=DEADLINE_SECONDS, max_processes=MAX_PROCESSES
    )
    processes, groups, requests = project(
        snapshot, protect_pids, os.getuid() if uid is None else uid
    )
    missing = sum(row["missing_reason"] is not None for row in processes)
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
        "kind": "memory_check.v1",
        "platform": "Darwin",
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
        "limitations": list(LIMITATIONS),
    }
    validate_memory_check(report)
    return report


def execute(request):
    if platform.system() != "Darwin":
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
        limitations=LIMITATIONS,
        details=report,
    )
