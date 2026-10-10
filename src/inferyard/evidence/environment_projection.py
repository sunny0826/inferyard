"""Periodic persistence only; live and endpoint snapshots stay complete (ADR 041)."""

_OMITTED_FIELDS = frozenset(
    {"cpu_flags", "os_release", "gpu", "mem_available_bytes", "page_size_bytes"}
)


def periodic_environment(snapshot: dict) -> dict:
    """Keep identity, policy, counters and source/read metadata without mutating input."""
    return {key: value for key, value in snapshot.items() if key not in _OMITTED_FIELDS}
