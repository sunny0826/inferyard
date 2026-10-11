"""Cross-record checks for memory diagnostics, including saved JSON."""

from pathlib import Path

from inferyard.contracts.validation import ContractError, validate_document


def _require(condition, path, reason):
    if not condition:
        raise ContractError(path, reason)


def windows_parent_verified(row, parent):
    return (
        parent is not None
        and row["start_identity"] is not None
        and row["parent_identity_missing_reason"] is None
        and parent["start_identity"] is not None
        and row["parent_start_identity"] == parent["start_identity"]
        and parent["start_identity"] <= row["start_identity"]
    )


def validate_memory_check(data):
    validate_document("memory_check", data)
    windows = data["kind"] == "memory_check.windows.v1"
    processes = {row["pid"]: row for row in data["processes"]}
    _require(len(processes) == len(data["processes"]), "processes", "duplicate PID")
    if windows:
        for row in processes.values():
            identity = row["parent_start_identity"]
            parent = processes.get(row["parent_pid"])
            _require(
                identity is None
                or (
                    row["start_identity"] is not None
                    and identity <= row["start_identity"]
                    and (parent is None or parent["start_identity"] in (None, identity))
                ),
                "processes.parent_start_identity",
                "invalid parent identity",
            )
            _require(
                not row["parent_pid"]
                or row["missing_reason"] is not None
                or identity is not None
                or row["parent_identity_missing_reason"] is not None,
                "processes.parent_identity_missing_reason",
                "unavailable parent identity requires reason",
            )
    groups = {row["group_id"]: row for row in data["groups"]}
    _require(len(groups) == len(data["groups"]), "groups", "duplicate group")
    requests = data["protected_requests"]
    _require(
        len({r["pid"] for r in requests}) == len(requests), "protected_requests", "duplicate PID"
    )
    for index, request in enumerate(requests):
        verified = request["status"] == "verified"
        _require(
            (request["start_identity"] is not None and request["reason"] is None)
            if verified
            else (request["start_identity"] is None and request["reason"] is not None),
            f"protected_requests[{index}]",
            "inconsistent protection identity",
        )
        if verified:
            row = processes.get(request["pid"])
            _require(
                row is not None
                and row["start_identity"] == request["start_identity"]
                and row["decision"]["status"] == "protected",
                f"protected_requests[{index}]",
                "protection not represented",
            )
    for reading in (data["memory_total"], data["memory_available"]):
        _reading(reading, "host_memory")
    coverage = data["coverage"]
    missing = sum(
        row["missing_reason"] is not None
        or row.get("parent_identity_missing_reason") is not None
        or (
            windows
            and (
                row["ownership"]["missing_reason"] is not None
                or row["private_commit"]["missing_reason"] is not None
            )
        )
        for row in processes.values()
    )
    _require(coverage["observed"] == len(processes), "coverage.observed", "count mismatch")
    _require(coverage["missing"] == missing, "coverage.missing", "count mismatch")
    _require(
        coverage["total_pids"] == coverage["observed"] + coverage["unobserved"],
        "coverage.total_pids",
        "count mismatch",
    )
    complete = (
        not missing
        and not coverage["unobserved"]
        and coverage["truncation_reason"] is None
        and all(r["status"] == "verified" for r in requests)
        and data["memory_total"]["value"] is not None
        and data["memory_available"]["value"] is not None
    )
    _require(coverage["complete"] == complete, "coverage.complete", "inconsistent completeness")
    members = set()
    for gid, group in groups.items():
        pids = group["member_pids"]
        _require(len(set(pids)) == len(pids), "groups", "duplicate member")
        _require(not members.intersection(pids), "groups", "member in multiple groups")
        _require(all(pid in processes for pid in pids), "groups", "unknown member")
        members.update(pids)
        rows = [processes[pid] for pid in pids]
        _require(all(row["group_id"] == gid for row in rows), "groups", "membership mismatch")
        expected_roots = sorted(row["pid"] for row in rows if row["parent_pid"] not in pids)
        if windows:
            expected_roots = sorted(
                row["pid"]
                for row in rows
                if row["parent_pid"] not in pids
                or not windows_parent_verified(row, processes.get(row["parent_pid"]))
            )
        _require(group["root_pids"] == expected_roots, "groups.root_pids", "root mismatch")
        values = [row["rss"]["value"] for row in rows]
        known = sum(value for value in values if value is not None)
        _require(group["rss_known_bytes"] == known, "groups.rss_known_bytes", "sum mismatch")
        rss_complete = all(value is not None for value in values)
        _require(group["rss_complete"] == rss_complete, "groups.rss_complete", "inconsistent sum")
        _require(
            group["rss_total_bytes"] == (known if rss_complete else None),
            "groups.rss_total_bytes",
            "inconsistent total",
        )
        if windows:
            values = [row["private_commit"]["value"] for row in rows]
            known_private = sum(v for v in values if v is not None)
            private_complete = all(v is not None for v in values)
            _require(
                group["private_commit_known_bytes"] == known_private
                and group["private_commit_complete"] == private_complete
                and group["private_commit_total_bytes"]
                == (known_private if private_complete else None),
                "groups.private_commit",
                "inconsistent total",
            )
        statuses = {row["decision"]["status"] for row in rows}
        expected = (
            "protected"
            if "protected" in statuses
            else "unknown"
            if "unknown" in statuses
            else "review"
        )
        _require(group["decision"]["status"] == expected, "groups.decision", "priority mismatch")
        expected_reasons = [
            reason
            for row in rows
            if row["decision"]["status"] == expected
            for reason in row["decision"]["reasons"]
        ]
        _require(
            sorted(group["decision"]["reasons"], key=lambda r: (str(r["pid"]), r["code"]))
            == sorted(expected_reasons, key=lambda r: (str(r["pid"]), r["code"])),
            "groups.decision.reasons",
            "reasons must represent member decisions",
        )
    for row in processes.values():
        _reading(row["rss"], "processes.rss")
        if windows:
            _windows_process(row)
        _require(
            row["start_identity"] is not None or row["missing_reason"] is not None,
            "processes.start_identity",
            "unavailable identity requires reason",
        )
        _require(
            (row["missing_reason"] is None) == (row["rss"]["value"] is not None),
            "processes.rss",
            "failed identity must discard measurement",
        )
        _require(
            row["parent_pid"] != 0 or row["parent_start_identity"] is None,
            "processes.parent_start_identity",
            "no parent must not have identity",
        )
        _require(
            all(r["pid"] == row["pid"] for r in row["decision"]["reasons"]),
            "processes.decision.reasons",
            "reason must reference this process",
        )
        _require(
            row["group_id"] is None or (row["group_id"] in groups and row["pid"] in members),
            "processes.group_id",
            "unknown group",
        )
        parent_missing = row.get("parent_identity_missing_reason")
        if parent_missing is not None:
            _require(
                row["parent_start_identity"] is None
                and row["start_identity"] is not None
                and row["missing_reason"] is None,
                "processes.parent_identity_missing_reason",
                "parent failure must not invalidate own identity or retain a parent edge",
            )
        if row["missing_reason"] is not None or parent_missing is not None:
            _require(row["decision"]["status"] != "review", "processes.decision", "missing checks")
        if row["decision"]["status"] == "review":
            _require(
                row["group_id"] is not None and row["start_identity"] is not None,
                "processes.decision",
                "review requires verified application identity",
            )


def _reading(reading, path):
    _require(
        (reading["value"] is None) == (reading["missing_reason"] is not None),
        path,
        "missing value requires reason; observed value must not have reason",
    )


def _windows_process(row):
    _reading(row["private_commit"], "processes.private_commit")
    _require(
        row["decision"]["status"] != "review",
        "processes.decision",
        "executable identity does not prove application ownership",
    )
    _require(
        row["missing_reason"] is None or row["private_commit"]["value"] is None,
        "processes.private_commit",
        "failed identity must discard measurement",
    )
    owner = row["ownership"]
    _require(
        (owner["relation"] == "unknown") == (owner["missing_reason"] is not None),
        "processes.ownership",
        "unknown ownership requires reason",
    )
    _require(
        owner["relation"] == "unknown" or owner["session_id"] is not None,
        "processes.ownership",
        "known owner requires session",
    )
    _require(
        row["group_id"] is None or owner["relation"] != "unknown",
        "processes.group_id",
        "unknown ownership must not establish executable group",
    )
    _require(
        row["identity_source"] == "GetProcessTimes:creation_FILETIME_100ns_since_1601",
        "processes.identity_source",
        "wrong Windows identity source",
    )


def read_memory_check(path: Path):
    from inferyard.evidence.storage import read_json

    data = read_json(path)
    validate_memory_check(data)
    return data
