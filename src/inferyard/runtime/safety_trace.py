"""Append each safety-check attempt without promising continuous temperature protection."""

import time
from datetime import UTC, datetime

from inferyard.platforms.identity import PreflightError


class SafetyTrace:
    def __init__(self, store, interval_seconds, *, clock=time.monotonic_ns):
        self.store = store
        self.clock = clock
        self.interval_ns = round(interval_seconds * 1e9)
        self.sequence = 0
        self.previous_periodic_start = None

    def check(self, guard, safety, *, periodic=False):
        started = self.clock()
        utc = datetime.now(UTC).isoformat()
        gap = None
        if periodic:
            if self.previous_periodic_start is not None:
                gap = started - self.previous_periodic_start
            self.previous_periodic_start = started
        self.sequence += 1
        # A failed prerequisite must not inherit the preceding sensor observation.
        safety.last = None
        error = None
        try:
            return guard()
        except BaseException as exc:
            error = {
                "type": type(exc).__name__,
                "reason": str(exc) if isinstance(exc, PreflightError) else None,
            }
            raise
        finally:
            self.store.observation(
                "safety-checks.jsonl",
                {
                    "definition": "safety_check_trace.v1",
                    "run_id": self.store.run_id,
                    "clock_id": self.store.clock_id,
                    "sequence": self.sequence,
                    "utc": utc,
                    "check_started_ns": started,
                    "check_finished_ns": self.clock(),
                    "trigger": "periodic" if periodic else "boundary",
                    "configured_interval_ns": self.interval_ns,
                    "periodic_start_gap_ns": gap,
                    "outcome": "error" if error else "passed",
                    "error": error,
                    "observation": safety.last,
                },
            )
