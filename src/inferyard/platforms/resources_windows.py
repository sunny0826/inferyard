"""Windows batch memory evidence with explicit unavailable resource metrics."""

import time
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.platforms.identity import PreflightError, process_start_ticks
from inferyard.platforms.telemetry import Sampler

COLLECTOR = "windows-resource.v1"
SOURCES = {
    "system_mem_available": "psutil:virtual_memory:available",
    "service_rss": "psutil:Process.memory_info:rss",
}
UNAVAILABLE = (
    "service_cpu_ticks",
    "system_swap_in",
    "system_swap_out",
    "temperature",
    "frequency",
    "gpu_memory",
    "gpu_utilization",
    "power",
    "energy",
)


def psutil_module():
    import psutil

    return psutil


class ResourceSampler(Sampler):
    def __init__(self, store, config, *, proc_root=Path("/proc"), clock=time.monotonic_ns):
        if proc_root != Path("/proc"):
            raise ValueError("Windows resources require the native platform source")
        super().__init__(store, config)
        self.clock = clock
        self.process_lost = False
        store.snapshot(
            "collector.json",
            {
                "schema_version": SCHEMA_VERSION,
                "collector": COLLECTOR,
                "interval_ms": config["telemetry"]["interval_ms"],
                "sources": SOURCES,
                "rss_scope": "bound_pid_excludes_child_processes_and_gpu_memory",
                "process_start_source": "GetProcessTimes:creation_FILETIME_100ns_since_1601",
                "sensors": {
                    "sources": [],
                    "discovery_issues": [{"reason": "windows_resource_sensors_not_collected"}],
                    "missing_metrics": ["temperature", "frequency", "power", "energy"],
                },
                "unavailable_metrics": {
                    metric: {
                        "value": None,
                        "missing_reason": "windows_resource_metric_not_collected",
                    }
                    for metric in UNAVAILABLE
                },
                "limitations": [
                    "sampled_memory_not_instantaneous_peak",
                    "external_cpu_and_boundary_environment_not_collected",
                    "strict_performance_comparison_not_qualified",
                ],
            },
        )

    async def run(self):
        from inferyard.runtime.environment_schedule import run_sampler

        await run_sampler(self)

    def _read(self, metric, pid, ticks):
        psutil = psutil_module()

        try:
            if metric == "service_rss":
                if self.process_lost or process_start_ticks(pid) != ticks:
                    raise PreflightError("source_changed")
                value = psutil.Process(pid).memory_info().rss
                if process_start_ticks(pid) != ticks:
                    raise PreflightError("source_changed")
            else:
                value = psutil.virtual_memory().available
            if type(value) is not int or value < 0:
                return None, "invalid_memory_value"
            return value, None
        except psutil.AccessDenied, PermissionError:
            return None, "permission_denied"
        except PreflightError as exc:
            self.process_lost |= metric == "service_rss"
            return None, "source_changed" if str(exc) == "source_changed" else "source_unavailable"
        except psutil.NoSuchProcess:
            self.process_lost |= metric == "service_rss"
            return None, "source_unavailable"
        except psutil.Error, OSError:
            return None, "source_unavailable"

    def collect(self, pid, ticks):
        samples = []
        for metric, source in SOURCES.items():
            started = self.clock()
            value, reason = self._read(metric, pid, ticks)
            service = metric == "service_rss"
            samples.append(
                {
                    "phase": self.phase,
                    "request_id": self.request_id,
                    "metric_name": metric,
                    "value": value,
                    "unit": "bytes",
                    "source": source,
                    "read_started_ns": started,
                    "read_finished_ns": self.clock(),
                    "server_pid": pid if service else None,
                    "process_start_ticks": ticks if service else None,
                    "missing_reason": reason,
                }
            )
        return samples
