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
PROCESS = obj(
    {
        "pid": NAT,
        "start_identity": nullable(POS),
        "identity_source": LABEL,
        "parent_pid": NAT,
        "parent_start_identity": nullable(POS),
        "display_name": LABEL,
        "rss": READING,
        "missing_reason": nullable(LABEL),
        "group_id": nullable(LABEL),
        "decision": DECISION,
    }
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
