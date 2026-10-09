"""Native Darwin resource evidence with explicit units and unavailable sensors."""

import os
import time
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.platforms.external_cpu_macos import TICKS_PER_SECOND, read_cpu
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.sensors_macos import MacSensors
from inferyard.platforms.telemetry import read_rss, sample_memory
from inferyard.runtime.environment_observer import EnvironmentObserver

COLLECTOR = "macos-resource.v1"
SOURCES = {
    "system_mem_available": "psutil:virtual_memory:available",
    "service_rss": "psutil:Process.memory_info:rss",
    "service_cpu_ticks": "psutil:Process.cpu_times:user+system",
    "system_swap_in": "vm_stat:Swapins",
    "system_swap_out": "vm_stat:Swapouts",
}


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
    ):
        if ticks_per_second not in (None, TICKS_PER_SECOND):
            raise ValueError("macOS CPU counters require microsecond scale")
        super().__init__(
            store, config, proc_root=proc_root, clock=clock, ticks_per_second=TICKS_PER_SECOND
        )
        try:
            self.page_size = os.sysconf("SC_PAGE_SIZE") if page_size is None else page_size
        except OSError, ValueError:
            self.page_size = None
        if type(self.page_size) is not int or self.page_size <= 0:
            self.page_size = None
        self.process_lost = False
        self.sensors = MacSensors()
        store.snapshot(
            "collector.json",
            {
                "schema_version": SCHEMA_VERSION,
                "collector": COLLECTOR,
                "sensors": self.sensors.metadata(),
                "clock_ticks_per_second": self.tick_rate,
                "page_size_bytes": self.page_size,
                "interval_ms": config["telemetry"]["interval_ms"],
                "boot_id": self.boot_id,
                "boot_source": "sysctl:kern.bootsessionuuid",
                "process_start_source": "psutil:macos:raw_kernel_starttime:microseconds",
                "cpu_scope": "bound_pid_all_threads_excludes_child_processes",
                "swap_scope": "host_swap_excludes_compression_not_model_attribution",
                "counter_timestamp": "read_interval_midpoint",
                "boundary_samples": "not_interpolated",
            },
        )

    def _counter_row(
        self,
        metric,
        value,
        reason,
        started,
        finished,
        capture,
        *,
        pid=None,
        ticks=None,
        raw_cpu=None,
    ):
        cpu = metric == "service_cpu_ticks"
        return {
            "phase": self.phase,
            "request_id": self.request_id,
            "metric_name": metric,
            "value": value,
            "unit": "ticks" if cpu else "pages",
            "source": SOURCES[metric],
            "read_started_ns": started,
            "read_finished_ns": finished,
            "server_pid": pid if cpu else None,
            "process_start_ticks": ticks if cpu else None,
            "missing_reason": reason,
            "collector": COLLECTOR,
            "boot_id": self.boot_id,
            "clock_ticks_per_second": self.tick_rate if cpu else None,
            "page_size_bytes": None if cpu else self.page_size,
            "raw_cpu": raw_cpu,
            "capture": capture,
        }

    def _cpu_counter(self, pid, ticks, capture):
        started = self.clock()
        value = raw_cpu = reason = None
        try:
            if self.boot_id is None:
                raise PreflightError("boot_identity_unavailable")
            if self.read_boot_id() != self.boot_id or self.process_lost:
                raise PreflightError("source_changed")
            raw_cpu = read_cpu(pid, ticks)
            value = sum(raw_cpu.values())
            if self.read_boot_id() != self.boot_id:
                raise PreflightError("source_changed")
        except PermissionError:
            reason = "permission_denied"
        except PreflightError as exc:
            reason = str(exc)
            self.process_lost |= reason == "source_changed"
        except OSError:
            reason = "source_unavailable"
        except ValueError, KeyError, OverflowError:
            reason = "invalid_counter"
        if reason:
            value = raw_cpu = None
        return self._counter_row(
            "service_cpu_ticks",
            value,
            reason,
            started,
            self.clock(),
            capture,
            pid=pid,
            ticks=ticks,
            raw_cpu=raw_cpu,
        )

    def _swap_counters(self, capture):
        from inferyard.platforms.macos_native import swap_snapshot

        started = self.clock()
        swap, common_reason = None, None
        try:
            if self.boot_id is None:
                raise PreflightError("boot_identity_unavailable")
            if self.read_boot_id() != self.boot_id:
                raise PreflightError("source_changed")
            if self.page_size is None:
                raise PreflightError("counter_scale_unavailable")
            swap = swap_snapshot()
            if swap["page_size_bytes"] != self.page_size:
                raise PreflightError("counter_scale_changed")
            if self.read_boot_id() != self.boot_id:
                raise PreflightError("source_changed")
        except PermissionError:
            common_reason = "permission_denied"
        except PreflightError as exc:
            common_reason = str(exc)
        except OSError:
            common_reason = "source_unavailable"
        except ValueError, KeyError, OverflowError:
            common_reason = "invalid_counter"
        values = []
        for metric, key in (("system_swap_in", "pswpin"), ("system_swap_out", "pswpout")):
            value, reason = None, common_reason
            if reason is None:
                try:
                    if swap["source"][key] != SOURCES[metric]:
                        raise PreflightError("source_changed")
                    value = swap[key]
                    if value is None:
                        raise PreflightError("source_unavailable")
                    if type(value) is not int or value < 0:
                        raise ValueError("invalid swap counter")
                except PreflightError as exc:
                    reason = str(exc)
                except ValueError, KeyError, OverflowError:
                    reason = "invalid_counter"
            values.append((metric, None if reason else value, reason))
        finished = self.clock()
        return [
            self._counter_row(metric, value, reason, started, finished, capture)
            for metric, value, reason in values
        ]

    def counters(self, pid, ticks, *, capture="periodic"):
        # The two swap values come from one fresh vm_stat invocation and share
        # its actual read/validation interval; no snapshot survives this call.
        return [self._cpu_counter(pid, ticks, capture), *self._swap_counters(capture)]

    def collect(self, pid, ticks):
        self.external(pid, ticks)
        return [
            *sample_memory(pid, ticks, self.phase, self.request_id),
            *self.counters(pid, ticks),
            *self.sensors.collect(self.phase, self.request_id),
        ]

    def idle_cycle_rss(self):
        endpoint = self.config["endpoint"]
        started = self.clock()
        value, reason = read_rss(endpoint["server_pid"], endpoint["process_start_ticks"])
        self.store.sample(
            {
                "phase": "residual",
                "request_id": self.request_id,
                "metric_name": "service_rss",
                "value": value,
                "unit": "bytes",
                "source": SOURCES["service_rss"] + ":cycle_confirmed_idle",
                "read_started_ns": started,
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
