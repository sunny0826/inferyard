"""Phase-labelled native memory sampling and environment interference observations."""

from __future__ import annotations

import asyncio
import os
import platform
import time
from pathlib import Path

from inferyard.platforms.identity import (
    PreflightError,
    environment_snapshot,
    memory_available,
    process_start_ticks,
)


def read_rss(pid: int, start_ticks: int, proc_root=Path("/proc")) -> tuple[int | None, str | None]:
    if (os.name == "nt" or platform.system() == "Darwin") and proc_root == Path("/proc"):
        import psutil

        try:
            if process_start_ticks(pid) != start_ticks:
                return None, "source_changed"
            value = psutil.Process(pid).memory_info().rss
            if process_start_ticks(pid) != start_ticks:
                return None, "source_changed"
            return (
                (value, None) if type(value) is int and value >= 0 else (None, "sensor_unavailable")
            )
        except OSError, psutil.Error, PreflightError:
            return None, "source_unavailable"
    try:
        if process_start_ticks(pid, proc_root) != start_ticks:
            return None, "source_changed"
        value = None
        for line in (proc_root / str(pid) / "status").read_text().splitlines():
            if line.startswith("VmRSS:"):
                fields = line.split()
                if len(fields) == 3 and fields[2] == "kB":
                    value = int(fields[1]) * 1024
        if process_start_ticks(pid, proc_root) != start_ticks:
            return None, "source_changed"
        return (value, None) if value is not None and value >= 0 else (None, "sensor_unavailable")
    except OSError, ValueError, PreflightError:
        return None, "source_unavailable"


def sample_memory(pid, start_ticks, phase, request_id, proc_root=Path("/proc")) -> list[dict]:
    samples = []
    windows = os.name == "nt" and proc_root == Path("/proc")
    macos = platform.system() == "Darwin" and proc_root == Path("/proc")
    for metric, source in (
        (
            "system_mem_available",
            "GlobalMemoryStatusEx:ullAvailPhys"
            if windows
            else "psutil:virtual_memory:available"
            if macos
            else "/proc/meminfo:MemAvailable",
        ),
        (
            "service_rss",
            "GetProcessMemoryInfo:WorkingSetSize"
            if windows
            else "psutil:Process.memory_info:rss"
            if macos
            else "/proc/<pid>/status:VmRSS",
        ),
    ):
        started = time.monotonic_ns()
        if metric == "service_rss":
            value, reason = read_rss(pid, start_ticks, proc_root)
        else:
            try:
                value, reason = memory_available(proc_root), None
            except PreflightError:
                value, reason = None, "sensor_unavailable"
        finished = time.monotonic_ns()
        samples.append(
            {
                "phase": phase,
                "request_id": request_id,
                "metric_name": metric,
                "value": value,
                "unit": "bytes",
                "source": source,
                "read_started_ns": started,
                "read_finished_ns": finished,
                "server_pid": pid if metric == "service_rss" else None,
                "process_start_ticks": start_ticks if metric == "service_rss" else None,
                "missing_reason": reason,
            }
        )
    return samples


class Sampler:
    def __init__(self, store, config):
        self.store = store
        self.config = config
        self.phase = "probe"
        self.request_id = None
        self.stopped = False
        self.failure = None

    def set_phase(self, phase, request_id=None):
        self.phase, self.request_id = phase, request_id

    def collect(self, pid, ticks):
        return sample_memory(pid, ticks, self.phase, self.request_id)

    async def run(self):
        loop = asyncio.get_running_loop()
        interval = self.config["telemetry"]["interval_ms"] / 1000
        due = loop.time()
        environment_due = due
        try:
            while not self.stopped:
                actual = loop.time()
                schedule = {
                    "scheduled_ns": int(due * 1e9),
                    "actual_ns": int(actual * 1e9),
                    "late_ns": int(max(0, actual - due) * 1e9),
                }
                pid = self.config["endpoint"]["server_pid"]
                ticks = self.config["endpoint"]["process_start_ticks"]
                for sample in self.collect(pid, ticks):
                    self.store.sample(sample)
                if actual >= environment_due:
                    observation = {
                        "monotonic_ns": time.monotonic_ns(),
                        "phase": self.phase,
                        "snapshot": environment_snapshot(),
                    }
                    self.store.observation("environment.jsonl", observation)
                    environment_due = actual + 1
                self.store.flush_due()
                schedule["collector_work_ns"] = int((loop.time() - actual) * 1e9)
                schedule["queue_depth"] = 0  # synchronous bounded writer, no hidden queue
                self.store.observation("schedule.jsonl", schedule)
                due += interval
                if due < loop.time():
                    # Do not manufacture samples for missed scheduled instants.
                    due += (int((loop.time() - due) // interval) + 1) * interval
                await asyncio.sleep(max(0, due - loop.time()))
        except Exception as exc:
            self.failure = exc
            raise
