"""Cross-record checks for memory diagnostics, including saved JSON."""

from pathlib import Path

from inferyard.contracts.validation import ContractError, validate_document


def _require(condition, path, reason):
    if not condition:
        raise ContractError(path, reason)


def validate_memory_check(data):
    validate_document("memory_check", data)
    processes = {row["pid"]: row for row in data["processes"]}
    _require(len(processes) == len(data["processes"]), "processes", "duplicate PID")
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
        row["missing_reason"] is not None or row.get("parent_identity_missing_reason") is not None
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


def read_memory_check(path: Path):
    from inferyard.evidence.storage import read_json

    data = read_json(path)
    validate_memory_check(data)
    return data
