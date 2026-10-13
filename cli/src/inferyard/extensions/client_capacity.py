"""Conservative load-client capacity evidence over the complete closed cohort."""

import asyncio
import os
import time

from inferyard.platforms.identity import process_start_ticks


class ClientCapacity:
    def __init__(self):
        self.begin = time.monotonic_ns()
        self.cpu_begin = time.process_time_ns()
        self.lags = []
        self.task = None

    async def monitor(self):
        while True:
            expected = time.monotonic_ns() + 5_000_000
            await asyncio.sleep(0.005)
            self.lags.append(max(0, time.monotonic_ns() - expected))

    def start(self):
        self.task = asyncio.create_task(self.monitor())

    async def close(self):
        self.task.cancel()
        await asyncio.gather(self.task, return_exceptions=True)
        return {
            "begin_ns": self.begin,
            "end_ns": time.monotonic_ns(),
            "client_pid": os.getpid(),
            "process_start_ticks": process_start_ticks(os.getpid()),
            "clock_id": self_clock(),
            "process_cpu_ns": time.process_time_ns() - self.cpu_begin,
            "event_loop_samples": len(self.lags),
            "max_event_loop_lag_ns": max(self.lags) if self.lags else None,
        }


def self_clock():
    import sys
    from pathlib import Path

    if sys.platform == "darwin":
        from inferyard.platforms.macos_native import boot_id

        return boot_id() + ":CLOCK_MONOTONIC"
    return Path("/proc/sys/kernel/random/boot_id").read_text().strip() + ":CLOCK_MONOTONIC"


def qualified(proof, window, spec):
    if (
        not proof
        or not window
        or not isinstance(proof.get("clock_id"), str)
        or not proof["clock_id"]
    ):
        return False
    keys = (
        "begin_ns",
        "end_ns",
        "client_pid",
        "process_start_ticks",
        "process_cpu_ns",
        "event_loop_samples",
        "max_event_loop_lag_ns",
    )
    if any(type(proof.get(k)) is not int or proof[k] < 0 for k in keys):
        return False
    span = proof["end_ns"] - proof["begin_ns"]
    return (
        span > 0
        and proof["begin_ns"] <= window[0] <= window[1] <= proof["end_ns"]
        and proof["client_pid"] > 0
        and proof["process_start_ticks"] > 0
        and proof["event_loop_samples"] >= 2
        and proof["process_cpu_ns"] / span <= spec["max_client_cpu_fraction"]
        and proof["max_event_loop_lag_ns"] <= spec["max_dispatch_delay_ns"]
    )
