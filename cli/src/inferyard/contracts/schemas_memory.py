"""Memory sampling shape shared by specialized collector payloads."""

from inferyard.contracts.schemas_common import (
    IDENTIFIER,
    LABEL,
    NAT,
    PHASE,
    POS,
    VERSION,
    enum,
    nullable,
    obj,
)

MEMORY_SAMPLE = obj(
    {
        "schema_version": VERSION,
        "experiment_id": IDENTIFIER,
        "trial_id": IDENTIFIER,
        "run_id": IDENTIFIER,
        "seq": POS,
        "clock_id": IDENTIFIER,
        "utc": LABEL,
        "phase": PHASE,
        "request_id": nullable(IDENTIFIER),
        "metric_name": enum("system_mem_available", "service_rss"),
        "value": nullable(NAT),
        "unit": enum("bytes"),
        "source": LABEL,
        "read_started_ns": NAT,
        "read_finished_ns": NAT,
        "server_pid": nullable(POS),
        "process_start_ticks": nullable(POS),
        "missing_reason": nullable(LABEL),
    }
)
