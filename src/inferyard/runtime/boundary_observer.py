"""Shared observations outside the measured request interval.

These guards can affect cache/spacing. They do not establish continuous absence
of interference; their CPU result is a frozen-width bracketing interval average.
"""

import asyncio
import time
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.platforms.external_cpu import capture, clock_ticks_per_second, read_boot_id
from inferyard.platforms.identity import environment_snapshot
from inferyard.platforms.resources import resource_collector_id
from inferyard.platforms.telemetry import Sampler


def boundary_contract(*, collector="linux-resource.v2"):
    if collector not in ("linux-resource.v2", "macos-resource.v1"):
        raise ValueError("unknown resource collector")
    macos = collector == "macos-resource.v1"
    return {
        "kind": "outside-request-environment.macos.v1"
        if macos
        else "outside-request-environment.v1",
        **({"collector": collector} if macos else {}),
        "boundaries": ["before_send", "after_terminal"],
        "sources": ["environment_snapshot", "host_minus_bound_pid_cpu"],
        "scope": "outside_request_timing_shared_guard_not_continuous_monitoring",
    }


class BoundaryGuard:
    def __init__(self, store, config, *, proc_root=Path("/proc")):
        self.store, self.config = store, config
        self.proc_root = proc_root
        self.hz = clock_ticks_per_second(proc_root)
        try:
            self.boot = read_boot_id(proc_root)
        except OSError, ValueError:
            self.boot = None
        store.snapshot(
            "boundary-observer.json",
            boundary_contract(collector=resource_collector_id(proc_root=proc_root)),
        )

    def observe(self, phase, request_id, boundary):
        started = time.monotonic_ns()
        environment = environment_snapshot()
        endpoint = self.config["endpoint"]
        cpu = capture(
            endpoint["server_pid"],
            endpoint["process_start_ticks"],
            self.boot,
            self.hz,
            phase,
            request_id,
            time.monotonic_ns,
            self.proc_root,
        )
        self.store.observation(
            "request-environment.jsonl",
            {
                "phase": phase,
                "request_id": request_id,
                "boundary": boundary,
                "read_started_ns": started,
                "read_finished_ns": time.monotonic_ns(),
                "environment": environment,
                "cpu": cpu,
            },
        )


class BoundaryOnlySampler(Sampler):
    """No in-request periodic observation or resource metrics."""

    def __init__(self, store, config):
        super().__init__(store, config)
        self.guard = BoundaryGuard(store, config)
        store.snapshot(
            "collector.json",
            {
                "schema_version": SCHEMA_VERSION,
                "collector": "boundary-only.v1",
                "scope": "outside_request_guards_only_no_periodic_collection",
            },
        )

    def before_request(self):
        self.guard.observe(self.phase, self.request_id, "before_send")

    def boundary(self, capture):
        if capture == "request_end":
            self.guard.observe(self.phase, self.request_id, "after_terminal")

    async def run(self):
        while not self.stopped:
            await asyncio.sleep(self.config["telemetry"]["interval_ms"] / 1000)
