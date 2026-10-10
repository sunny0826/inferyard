"""Read-only Linux cumulative counters with process/boot and conversion identities."""

import os
import time
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.platforms.identity import PreflightError, process_start_ticks
from inferyard.platforms.sensors_linux import LinuxSensors
from inferyard.platforms.telemetry import read_rss, sample_memory
from inferyard.runtime.environment_observer import EnvironmentObserver


def read_cpu(pid, ticks, proc_root):
    # /proc/PID/stat comm may contain ')' or spaces; fields start at field 3.
    raw = (proc_root / str(pid) / "stat").read_text().rsplit(")", 1)[1].split()
    if int(raw[19]) != ticks:
        raise PreflightError("source_changed")
    user, system = int(raw[11]), int(raw[12])
    if min(user, system) < 0:
        raise ValueError("negative CPU counter")
    if process_start_ticks(pid, proc_root) != ticks:
        raise PreflightError("source_changed")
    return {"user_ticks": user, "system_ticks": system}


def read_swap(entries, key):
    values = [int(parts[1]) for parts in entries if len(parts) == 2 and parts[0] == key]
    if len(values) != 1 or values[0] < 0:
        raise ValueError("invalid swap counter")
    return values[0]


class ResourceSampler(EnvironmentObserver):
    def __init__(
        self,
        store,
        config,
        *,
        proc_root=Path("/proc"),
        clock=time.monotonic_ns,
        ticks_per_second=None,
        page_size=None,
        sys_root=Path("/sys"),
    ):
        super().__init__(
            store, config, proc_root=proc_root, clock=clock, ticks_per_second=ticks_per_second
        )
        self.page_size = page_size
        if self.page_size is None and hasattr(os, "sysconf"):
            self.page_size = os.sysconf("SC_PAGE_SIZE")
        self.page_size = (
            self.page_size if type(self.page_size) is int and self.page_size > 0 else None
        )
        self.sensors = LinuxSensors(sys_root, clock=clock)
        self.process_lost = False
        store.snapshot(
            "collector.json",
            {
                "schema_version": SCHEMA_VERSION,
                "sensors": self.sensors.metadata(),
                "collector": "linux-resource.v2",
                "clock_ticks_per_second": self.tick_rate,
                "page_size_bytes": self.page_size,
                "interval_ms": config["telemetry"]["interval_ms"],
                "boot_id": self.boot_id,
                "cpu_scope": "bound_pid_all_threads_excludes_child_processes",
                "swap_scope": "host_including_swap_and_zram_not_model_attribution",
                "counter_timestamp": "read_interval_midpoint",
                "boundary_samples": "not_interpolated",
            },
        )

    def counters(self, pid, ticks, *, capture="periodic"):
        samples = []
        swap = None
        for metric, source in (
            ("service_cpu_ticks", "/proc/<pid>/stat:utime+stime"),
            ("system_swap_in", "/proc/vmstat:pswpin"),
            ("system_swap_out", "/proc/vmstat:pswpout"),
        ):
            cpu = metric == "service_cpu_ticks"
            start = self.clock() if cpu or swap is None else swap[0]
            value = raw_cpu = reason = None
            if not cpu and swap is not None:
                start, finished, entries, shared_reason = swap
            else:
                entries = None
                shared_reason = None
            try:
                if not cpu and swap is not None:
                    if shared_reason:
                        raise PreflightError(shared_reason)
                    value = read_swap(entries, "pswpout")
                else:
                    value, raw_cpu, entries = self._read_counter(cpu, pid, ticks)
                    if not cpu:
                        value = read_swap(entries, "pswpin")
            except PermissionError:
                reason = "permission_denied"
            except PreflightError as exc:
                reason = str(exc)
                self.process_lost |= cpu and reason == "source_changed"
            except OSError:
                reason = "source_unavailable"
            except ValueError, IndexError:
                reason = "invalid_counter"
            if cpu or swap is None:
                finished = self.clock()
            if not cpu and swap is None:
                swap = (start, finished, entries, reason if entries is None else None)
            if reason:
                value = raw_cpu = None
            samples.append(
                self._sample(
                    metric, source, pid, ticks, capture, start, finished, value, raw_cpu, reason
                )
            )
        return samples

    def _read_counter(self, cpu, pid, ticks):
        if self.boot_id is None:
            raise PreflightError("boot_identity_unavailable")
        if (self.tick_rate if cpu else self.page_size) is None:
            raise PreflightError("counter_scale_unavailable")
        if self.read_boot_id() != self.boot_id:
            raise PreflightError("source_changed")
        raw_cpu = entries = None
        if cpu:
            if self.process_lost:
                raise PreflightError("source_changed")
            raw_cpu = read_cpu(pid, ticks, self.proc_root)
            value = sum(raw_cpu.values())
        else:
            entries = [
                line.split() for line in (self.proc_root / "vmstat").read_text().splitlines()
            ]
            value = None
        if self.read_boot_id() != self.boot_id:
            raise PreflightError("source_changed")
        return value, raw_cpu, entries

    def _sample(self, metric, source, pid, ticks, capture, start, finished, value, raw_cpu, reason):
        cpu = metric == "service_cpu_ticks"
        return {
            "phase": self.phase,
            "request_id": self.request_id,
            "metric_name": metric,
            "value": value,
            "unit": "ticks" if cpu else "pages",
            "source": source,
            "read_started_ns": start,
            "read_finished_ns": finished,
            "server_pid": pid if cpu else None,
            "process_start_ticks": ticks if cpu else None,
            "missing_reason": reason,
            "collector": "linux-resource.v2",
            "boot_id": self.boot_id,
            "clock_ticks_per_second": self.tick_rate if cpu else None,
            "page_size_bytes": None if cpu else self.page_size,
            "raw_cpu": raw_cpu,
            "capture": capture,
        }

    def collect(self, pid, ticks):
        self.external(pid, ticks)
        return [
            *sample_memory(pid, ticks, self.phase, self.request_id, self.proc_root),
            *self.counters(pid, ticks),
            *self.sensors.collect(self.phase, self.request_id),
        ]

    def idle_cycle_rss(self):
        endpoint = self.config["endpoint"]
        start = self.clock()
        value, reason = read_rss(
            endpoint["server_pid"], endpoint["process_start_ticks"], self.proc_root
        )
        self.store.sample(
            {
                "phase": "residual",
                "request_id": self.request_id,
                "metric_name": "service_rss",
                "value": value,
                "unit": "bytes",
                "source": "/proc/<pid>/status:VmRSS:cycle_confirmed_idle",
                "read_started_ns": start,
                "read_finished_ns": self.clock(),
                "server_pid": endpoint["server_pid"],
                "process_start_ticks": endpoint["process_start_ticks"],
                "missing_reason": reason,
            }
        )

    def boundary(self, capture):
        started = self.clock()
        endpoint = self.config["endpoint"]
        if capture == "request_end":
            self.external(endpoint["server_pid"], endpoint["process_start_ticks"])
            self.boundary_guard.observe(self.phase, self.request_id, "after_terminal")
        for sample in self.counters(
            endpoint["server_pid"], endpoint["process_start_ticks"], capture=capture
        ):
            self.store.sample(sample)
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
