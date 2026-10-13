"""Application grouping and conservative decisions from observed process identities."""

import hashlib
from pathlib import PurePosixPath

from inferyard.platforms.memory_macos import IDENTITY_SOURCE

PROTECTED_APPS = {
    "lody",
    "cursor",
    "visual studio code",
    "terminal",
    "iterm",
    "iterm2",
    "warp",
    "lm studio",
    "raycast",
    "clash verge",
    "uuremote",
    "rustdesk",
    "anydesk",
    "teamviewer",
    "magpie",
    "kimi",
    "kimicu",
    "grok bot",
}
PROTECTED_BINARIES = {"codex", "claude", "lody", "llama-server", "ollama", "sshd", "ssh", "mosh"}


def decision(status, code, pid=None):
    return {"status": status, "reasons": [{"code": code, "pid": pid}]}


def _edge(row, parent):
    return (
        parent is not None
        and row.get("parent_identity_missing_reason") is None
        and row["start_identity"] is not None
        and parent["start_identity"] is not None
        and row["parent_start_identity"] == parent["start_identity"]
    )


def _ancestors(pid, rows):
    seen = set()
    while pid in rows and pid not in seen:
        seen.add(pid)
        row = rows[pid]
        parent = rows.get(row["parent_pid"])
        if not _edge(row, parent):
            break
        pid = parent["pid"]
    return seen


def _protected_reason(row, uid):
    if row["pid"] == 0:
        return "system_process"
    if row["uid"] is not None and row["uid"] != uid:
        return "other_account"
    executable = row["executable"]
    if executable:
        path = PurePosixPath(executable)
        if str(path).startswith(
            ("/System/", "/usr/bin/", "/usr/sbin/", "/usr/libexec/", "/sbin/", "/bin/")
        ):
            return "system_process"
        if ".lody" in path.parts or path.name.lower() in PROTECTED_BINARIES:
            return "agent_or_service"
    bundle = row["bundle"]
    if bundle and bundle["name"].lower() in PROTECTED_APPS:
        return "agent_service_or_connection_application"
    return None


def project(snapshot, protect_pids, uid):
    rows = {row["pid"]: row for row in snapshot["rows"]}
    protected = {}
    for pid in _ancestors(snapshot["caller_pid"], rows):
        protected[pid] = "collector_or_caller_ancestor"
    requests = []
    for pid in sorted(set(protect_pids)):
        row = rows.get(pid)
        verified = (
            row is not None and row["start_identity"] is not None and not row["missing_reason"]
        )
        requests.append(
            {
                "pid": pid,
                "start_identity": row["start_identity"] if verified else None,
                "status": "verified" if verified else "unavailable",
                "reason": None if verified else "protection_identity_unavailable",
            }
        )
        if verified:
            protected[pid] = "declared_protection"
    for pid, row in rows.items():
        if reason := _protected_reason(row, uid):
            protected.setdefault(pid, reason)
    # Descendants inherit protection, never siblings sharing a common ancestor.
    # Caller ancestors are protected themselves but do not protect all their children.
    for pid in rows:
        ancestors = _ancestors(pid, rows) - {pid}
        if any(
            protected.get(p)
            not in (None, "collector_or_caller_ancestor", "system_process", "other_account")
            for p in ancestors
        ):
            protected.setdefault(pid, "protected_descendant")
    incomplete_classification = (
        snapshot["truncation_reason"] is not None
        or any(r["status"] != "verified" for r in requests)
        or any(
            row["missing_reason"] not in (None, "process_exited")
            and row["uid"] in (None, uid)
            and row["pid"] not in protected
            for row in rows.values()
        )
    )
    assignments, sources = {}, {}
    for pid, row in rows.items():
        if row["bundle"]:
            assignments[pid] = row["bundle"]
            sources[pid] = "verified_bundle"
    native_bundles = assignments.copy()
    for pid, row in rows.items():
        parents = _ancestors(pid, rows) - {pid}
        bundles = {
            native_bundles[p]["key"]: native_bundles[p] for p in parents if p in native_bundles
        }
        own = assignments.get(pid)
        if len(bundles) > 1 or (own and bundles and own["key"] not in bundles):
            assignments.pop(pid, None)
            sources.pop(pid, None)
        elif not own and len(bundles) == 1 and row["missing_reason"] is None:
            assignments[pid] = next(iter(bundles.values()))
            sources[pid] = "verified_bundle_and_parent"
    processes, groups = [], {}
    for pid, row in rows.items():
        bundle = assignments.get(pid)
        gid = "app-" + hashlib.sha256(bundle["key"].encode()).hexdigest()[:24] if bundle else None
        if pid in protected:
            selected = decision("protected", protected[pid], pid)
        elif (
            row["missing_reason"]
            or row.get("parent_identity_missing_reason")
            or incomplete_classification
        ):
            selected = decision(
                "unknown",
                row["missing_reason"]
                or row.get("parent_identity_missing_reason")
                or "incomplete_protection_checks",
                pid,
            )
        elif bundle and row["uid"] == uid:
            selected = decision("review", "verified_application_requires_user_review", pid)
        else:
            selected = decision("unknown", "application_or_dependency_unverified", pid)
        name = bundle["name"] if bundle else row["name"]
        name = "".join(c for c in name if c.isprintable())[:128] or "unavailable"
        public = {
            "pid": pid,
            "start_identity": row["start_identity"],
            "identity_source": IDENTITY_SOURCE,
            "parent_pid": row["parent_pid"],
            "parent_start_identity": row["parent_start_identity"],
            "parent_identity_missing_reason": row.get("parent_identity_missing_reason"),
            "display_name": name,
            "rss": row["rss"],
            "missing_reason": row["missing_reason"],
            "group_id": gid,
            "decision": selected,
        }
        processes.append(public)
        if gid:
            groups.setdefault(gid, {"group_id": gid, "display_name": name, "members": []})[
                "members"
            ].append(public)
    output_groups = []
    for group in groups.values():
        members = group.pop("members")
        pids = {m["pid"] for m in members}
        states = {m["decision"]["status"] for m in members}
        status = (
            "protected" if "protected" in states else "unknown" if "unknown" in states else "review"
        )
        reasons = [
            r
            for m in members
            if m["decision"]["status"] == status
            for r in m["decision"]["reasons"]
        ]
        values = [m["rss"]["value"] for m in members]
        known = sum(v for v in values if v is not None)
        complete = all(v is not None for v in values)
        output_groups.append(
            {
                **group,
                "member_pids": sorted(pids),
                "root_pids": sorted(m["pid"] for m in members if m["parent_pid"] not in pids),
                "grouping_source": "verified_bundle_and_parent"
                if any(sources[p] != "verified_bundle" for p in pids)
                else "verified_bundle",
                "rss_known_bytes": known,
                "rss_total_bytes": known if complete else None,
                "rss_complete": complete,
                "decision": {"status": status, "reasons": reasons},
            }
        )
    return (
        sorted(processes, key=lambda p: p["pid"]),
        sorted(output_groups, key=lambda g: -g["rss_known_bytes"]),
        requests,
    )
