"""Conservative executable groups, never a claim of complete Windows app ownership."""

import hashlib
from pathlib import PureWindowsPath

from inferyard.platforms.memory_windows import IDENTITY_SOURCE

PROTECTED_NAMES = {
    "system",
    "registry",
    "secure system",
    "memory compression",
    "smss.exe",
    "csrss.exe",
    "wininit.exe",
    "winlogon.exe",
    "services.exe",
    "svchost.exe",
    "lsass.exe",
    "lsaiso.exe",
    "msmpeng.exe",
    "nissrv.exe",
    "mssense.exe",
    "securityhealthservice.exe",
    "securityhealthsystray.exe",
    "sgrmbroker.exe",
    "senseir.exe",
    "sensece.exe",
    "codex.exe",
    "claude.exe",
    "lody.exe",
    "cursor.exe",
    "code.exe",
    "dimagent.exe",
    "kimi.exe",
    "kimicu.exe",
    "grokbot.exe",
    "llama-server.exe",
    "ollama.exe",
    "lm studio.exe",
    "ssh.exe",
    "sshd.exe",
    "mosh.exe",
    "powershell.exe",
    "pwsh.exe",
    "cmd.exe",
    "windowsterminal.exe",
    "conhost.exe",
    "openconsole.exe",
    "rustdesk.exe",
    "anydesk.exe",
    "teamviewer.exe",
    "uuremote.exe",
}


def _ancestors(pid, rows):
    seen = set()
    while pid in rows and pid not in seen:
        seen.add(pid)
        row = rows[pid]
        parent = rows.get(row["parent_pid"])
        if (
            not parent
            or row["start_identity"] is None
            or row["parent_identity_missing_reason"] is not None
            or parent["start_identity"] is None
            or row["parent_start_identity"] != parent["start_identity"]
            or parent["start_identity"] > row["start_identity"]
        ):
            break
        pid = parent["pid"]
    return seen


def _reason(row, snapshot):
    if row["pid"] in (0, 4):
        return "system_process"
    if row["session"] == 0:
        return "service_session"
    if row["owner"] is not None and snapshot["caller_owner"] is not None:
        if row["owner"] != snapshot["caller_owner"]:
            return "other_account"
        if row["session"] != snapshot["caller_session"]:
            return "other_session"
    path = PureWindowsPath(row["executable"] or "")
    if (
        row["name"].casefold() in PROTECTED_NAMES
        or path.name.casefold() in PROTECTED_NAMES
        or any(
            part.casefold()
            in (
                ".lody",
                ".codex",
                ".claude",
                "windows defender",
                "windows defender advanced threat protection",
            )
            for part in path.parts
        )
    ):
        return "agent_security_service_or_connection"
    root = snapshot.get("system_root")
    if root and path.is_relative_to(PureWindowsPath(root)):
        return "system_process"
    return None


def missing(row):
    return bool(
        row["missing_reason"]
        or row.get("parent_identity_missing_reason")
        or row["ownership"]["missing_reason"]
        or row["private_commit"]["missing_reason"]
    )


def project(snapshot, protect_pids):
    rows = {row["pid"]: row for row in snapshot["rows"]}
    protected = {}
    for caller in (snapshot["caller_pid"], snapshot["collector_pid"]):
        for pid in _ancestors(caller, rows):
            protected[pid] = "collector_or_caller_ancestor"
    requests = []
    for pid in sorted(set(protect_pids)):
        row = rows.get(pid)
        verified = row is not None and row["start_identity"] is not None
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
        if reason := _reason(row, snapshot):
            if protected.get(pid) == "collector_or_caller_ancestor" and reason != "system_process":
                protected[pid] = reason
            else:
                protected.setdefault(pid, reason)
    roots = protected.copy()
    for pid in rows:
        if any(
            roots.get(p) not in (None, "collector_or_caller_ancestor", "system_process")
            for p in _ancestors(pid, rows) - {pid}
        ):
            protected.setdefault(pid, "protected_descendant")
    processes, grouped = [], {}
    for pid, row in rows.items():
        relation = "unknown"
        owner_reason = row["ownership_missing_reason"]
        if row["owner"] is not None and snapshot["caller_owner"] is not None:
            relation = (
                "same_account" if row["owner"] == snapshot["caller_owner"] else "other_account"
            )
        else:
            owner_reason = owner_reason or "caller_ownership_unavailable"
        image = row["image"]
        # Neither display-name equality nor a parent PID alone proves common ownership.
        key = (
            f"{image['key']}:{row['owner']}:{row['session']}"
            if image and relation != "unknown" and not owner_reason
            else None
        )
        gid = "exe-" + hashlib.sha256(key.encode()).hexdigest()[:24] if key else None
        code = (
            protected.get(pid)
            or row["missing_reason"]
            or owner_reason
            or (row["parent_identity_missing_reason"] or "application_ownership_unverified")
        )
        name = "".join(c for c in row["name"] if c.isprintable())[:128] or "unavailable"
        public = {
            "pid": pid,
            "start_identity": row["start_identity"],
            "identity_source": IDENTITY_SOURCE,
            "parent_pid": row["parent_pid"],
            "parent_start_identity": row["parent_start_identity"],
            "parent_identity_missing_reason": row["parent_identity_missing_reason"],
            "display_name": name,
            "rss": row["rss"],
            "private_commit": row["private_commit"],
            "ownership": {
                "relation": relation,
                "session_id": row["session"],
                "missing_reason": owner_reason,
            },
            "missing_reason": row["missing_reason"],
            "group_id": gid,
            "decision": {
                "status": "protected" if pid in protected else "unknown",
                "reasons": [{"code": code, "pid": pid}],
            },
        }
        processes.append(public)
        if gid:
            grouped.setdefault(gid, []).append(public)
    groups = []
    for gid, members in grouped.items():
        pids = {m["pid"] for m in members}
        status = (
            "protected"
            if any(m["decision"]["status"] == "protected" for m in members)
            else "unknown"
        )
        group = {
            "group_id": gid,
            "display_name": members[0]["display_name"],
            "member_pids": sorted(pids),
            "root_pids": sorted(m["pid"] for m in members if m["parent_pid"] not in pids),
            "grouping_source": "verified_executable_owner_session",
            "decision": {
                "status": status,
                "reasons": [
                    r
                    for m in members
                    if m["decision"]["status"] == status
                    for r in m["decision"]["reasons"]
                ],
            },
        }
        for field in ("rss", "private_commit"):
            values = [m[field]["value"] for m in members]
            known = sum(v for v in values if v is not None)
            complete = all(v is not None for v in values)
            group.update(
                {
                    f"{field}_known_bytes": known,
                    f"{field}_total_bytes": known if complete else None,
                    f"{field}_complete": complete,
                }
            )
        groups.append(group)
    return (
        sorted(processes, key=lambda p: p["pid"]),
        sorted(groups, key=lambda g: -g["rss_known_bytes"]),
        requests,
    )
