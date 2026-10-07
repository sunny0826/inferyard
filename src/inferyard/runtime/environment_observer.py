"""Common environment observer for measured and resource-disabled v2 arms.

The observer remains enabled in both arms. ABBA measures only the incremental
resource collector perturbation; observer perturbation is not thereby measured.
"""

import time
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.platforms.external_cpu import capture as capture_external_cpu
from inferyard.platforms.external_cpu import clock_ticks_per_second, read_boot_id
from inferyard.platforms.resources import resource_collector_id
from inferyard.platforms.telemetry import Sampler
from inferyard.runtime.boundary_observer import BoundaryGuard


def observer_contract(interval_ms, *, collector="linux-resource.v2"):
    if collector not in ("linux-resource.v2", "macos-resource.v1"):
        raise ValueError("unknown resource collector")
    macos = collector == "macos-resource.v1"
    return {
        "kind": "macos-environment-observer.v1" if macos else "linux-environment-observer.v1",
        **({"collector": collector} if macos else {}),
        "environment_interval_ms": 1000,
        "external_cpu_interval_ms": interval_ms,
        "external_cpu_boundary": "request_end",
        "scope": "environment_identity_and_host_minus_bound_pid_cpu",
        "cost_scope": "present_in_both_arms_not_measured_by_incremental_abba",
    }


class EnvironmentObserver(Sampler):
    async def run(self):
        from inferyard.runtime.environment_schedule import run_sampler

        await run_sampler(self)

    def __init__(
        self,
        store,
        config,
        *,
        proc_root=Path("/proc"),
        clock=time.monotonic_ns,
        ticks_per_second=None,
    ):
        super().__init__(store, config)
        self.proc_root, self.clock = proc_root, clock
        rate = ticks_per_second
        if rate is None:
            rate = clock_ticks_per_second(proc_root)
        self.tick_rate = rate if type(rate) is int and rate > 0 else None
        try:
            self.boot_id = self.read_boot_id()
        except OSError, ValueError:
            self.boot_id = None
        collector = resource_collector_id(proc_root=proc_root)
        store.snapshot(
            "observer.json",
            observer_contract(config["telemetry"]["interval_ms"], collector=collector),
        )
        self.boundary_guard = BoundaryGuard(store, config, proc_root=proc_root)

    def before_request(self):
        self.boundary_guard.observe(self.phase, self.request_id, "before_send")

    def read_boot_id(self):
        return read_boot_id(self.proc_root)

    def external(self, pid, ticks):
        self.store.observation(
            "external-cpu.jsonl",
            capture_external_cpu(
                pid,
                ticks,
                self.boot_id,
                self.tick_rate,
                self.phase,
                self.request_id,
                self.clock,
                self.proc_root,
            ),
        )

    def collect(self, pid, ticks):
        self.external(pid, ticks)
        return []

    def boundary(self, capture):
        started = self.clock()
        if capture == "request_end":
            endpoint = self.config["endpoint"]
            self.external(endpoint["server_pid"], endpoint["process_start_ticks"])
            self.boundary_guard.observe(self.phase, self.request_id, "after_terminal")
        self.store.observation(
            "schedule.jsonl",
            {
                "phase": self.phase,
                "request_id": self.request_id,
                "capture": capture,
                "read_started_ns": started,
                "read_finished_ns": self.clock(),
                "kind": "resource_boundary",
            },
        )


class EnvironmentOnlySampler(EnvironmentObserver):
    """No resource metric samples; preserve the same observer as the on arm."""

    def __init__(self, store, config, **kwargs):
        super().__init__(store, config, **kwargs)
        store.snapshot(
            "collector.json",
            {
                "schema_version": SCHEMA_VERSION,
                "collector": "environment-only.v1",
                "scope": "resource_metrics_disabled_common_environment_observer_enabled",
            },
        )
