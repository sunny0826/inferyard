"""Portable structure for the standalone, read-only memory diagnostic."""

from inferyard.contracts.schemas_common import (
    BOOL,
    LABEL,
    NAT,
    POS,
    VERSION,
    array,
    enum,
    nullable,
    obj,
)

READING = obj({"value": nullable(NAT), "source": LABEL, "missing_reason": nullable(LABEL)})
REASON = obj({"code": LABEL, "pid": nullable(NAT)})
DECISION = obj(
    {"status": enum("protected", "unknown", "review"), "reasons": array(REASON, minimum=1)}
)
PROCESS_FIELDS = {
    "pid": NAT,
    "start_identity": nullable(POS),
    "identity_source": LABEL,
    "parent_pid": NAT,
    "parent_start_identity": nullable(POS),
    "parent_identity_missing_reason": nullable(LABEL),
    "display_name": LABEL,
    "rss": READING,
    "missing_reason": nullable(LABEL),
    "group_id": nullable(LABEL),
    "decision": DECISION,
}
PROCESS = obj(
    PROCESS_FIELDS,
    required=[name for name in PROCESS_FIELDS if name != "parent_identity_missing_reason"],
)
GROUP = obj(
    {
        "group_id": LABEL,
        "display_name": LABEL,
        "member_pids": array(NAT, minimum=1),
        "root_pids": array(NAT),
        "grouping_source": enum("verified_bundle", "verified_bundle_and_parent"),
        "rss_known_bytes": NAT,
        "rss_total_bytes": nullable(NAT),
        "rss_complete": BOOL,
        "decision": DECISION,
    }
)
MEMORY_CHECK = obj(
    {
        "schema_version": VERSION,
        "kind": enum("memory_check.v1"),
        "platform": enum("Darwin"),
        "captured_at": LABEL,
        "collection_ns": NAT,
        "memory_total": READING,
        "memory_available": READING,
        "coverage": obj(
            {
                "total_pids": NAT,
                "observed": NAT,
                "missing": NAT,
                "unobserved": NAT,
                "complete": BOOL,
                "truncation_reason": nullable(enum("deadline", "process_limit")),
            }
        ),
        "budget": obj({"deadline_seconds": POS, "max_processes": POS}),
        "protected_requests": array(
            obj(
                {
                    "pid": POS,
                    "start_identity": nullable(POS),
                    "status": enum("verified", "unavailable"),
                    "reason": nullable(LABEL),
                }
            )
        ),
        "processes": array(PROCESS),
        "groups": array(GROUP),
        "limitations": array(LABEL),
    }
)

# Keep the complete macOS v1 branch byte-for-byte compatible. Windows has its own
# kind because ownership and completeness add measurement requirements.
WINDOWS_PROCESS = obj(
    {
        **PROCESS_FIELDS,
        "private_commit": READING,
        "ownership": obj(
            {
                "relation": enum("same_account", "other_account", "unknown"),
                "session_id": nullable(NAT),
                "missing_reason": nullable(LABEL),
            }
        ),
    }
)
WINDOWS_GROUP = obj(
    {
        **GROUP["properties"],
        "grouping_source": enum("verified_executable_owner_session"),
        "private_commit_known_bytes": NAT,
        "private_commit_total_bytes": nullable(NAT),
        "private_commit_complete": BOOL,
    }
)
WINDOWS_MEMORY_CHECK = obj(
    {
        **MEMORY_CHECK["properties"],
        "kind": enum("memory_check.windows.v1"),
        "platform": enum("Windows"),
        "processes": array(WINDOWS_PROCESS),
        "groups": array(WINDOWS_GROUP),
    }
)
MEMORY_CHECK = {"anyOf": [MEMORY_CHECK, WINDOWS_MEMORY_CHECK]}
