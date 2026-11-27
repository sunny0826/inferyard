"""Phase-labelled native memory sampling and environment interference observations."""

from __future__ import annotations

import asyncio
import os
import platform
import time
from pathlib import Path

from inferyard.errors import PreflightError
from inferyard.platforms.environment_projection import periodic_environment
from inferyard.platforms.identity import environment_snapshot, memory_available, process_start_ticks


def bound_workers(config: dict | None) -> tuple[tuple[int, int], ...]:
    if not isinstance(config, dict):
        return ()
    endpoint = config.get("endpoint")
    if not isinstance(endpoint, dict):
        return ()
    binding_path = endpoint.get("binding_path")
    binding_sha = endpoint.get("binding_sha256")
    if not binding_path or not binding_sha:
        return ()
    try:
        from inferyard.platforms.local_engines.assets import bound_json

        binding, _ = bound_json(binding_path, binding_sha)
    except Exception:
        return ()
    service = binding.get("service") if isinstance(binding, dict) else None
    raw_workers = service.get("workers") if isinstance(service, dict) else None
    if not isinstance(raw_workers, list):
        return ()
    root_pid = endpoint.get("server_pid")
    seen: set[int] = {root_pid} if type(root_pid) is int else set()
    workers: list[tuple[int, int]] = []
    for item in raw_workers:
        if not isinstance(item, dict):
            continue
        wpid = item.get("pid")
        wticks = item.get("start_ticks")
        if (
            type(wpid) is int
            and wpid > 0
            and type(wticks) is int
            and wticks > 0
            and wpid not in seen
        ):
            seen.add(wpid)
            workers.append((wpid, wticks))
    return tuple(workers)


def _read_single_rss(
    pid: int, start_ticks: int, proc_root: Path = Path("/proc")
) -> tuple[int | None, str | None]:
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


def read_rss(
    pid: int,
    start_ticks: int,
    proc_root=Path("/proc"),
    *,
    workers: tuple[tuple[int, int], ...] = (),
) -> tuple[int | None, str | None]:
    total, reason = _read_single_rss(pid, start_ticks, proc_root)
    if reason is not None or total is None:
        return None, reason
    for wpid, wticks in workers:
        wval, wreason = _read_single_rss(wpid, wticks, proc_root)
        if wreason is not None or wval is None:
            return None, wreason
        total += wval
    return total, None


def sample_memory(
    pid,
    start_ticks,
    phase,
    request_id,
    proc_root=Path("/proc"),
    *,
    workers: tuple[tuple[int, int], ...] = (),
) -> list[dict]:
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
            value, reason = read_rss(pid, start_ticks, proc_root, workers=workers)
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
        self.workers = bound_workers(config)
        self.phase = "probe"
        self.request_id = None
        self.stopped = False
        self.failure = None
        self.environment_constants = None

    def use_start_environment(self, snapshot):
        from inferyard.platforms.environment_constants import capture_constants

        self.environment_constants = capture_constants(snapshot)

    @property
    def stopped(self):
        return self._stopped

    @stopped.setter
    def stopped(self, value):
        self._stopped = value
        if hasattr(self, "_stop_event"):
            if value:
                self._stop_event.set()
            else:
                self._stop_event.clear()

    async def wait(self, seconds):
        """Wake on stop without cancelling a collection already in progress."""
        if self.stopped:
            return
        if seconds <= 0:
            await asyncio.sleep(0)
            return
        if not hasattr(self, "_stop_event"):
            self._stop_event = asyncio.Event()
        try:
            await asyncio.wait_for(self._stop_event.wait(), timeout=seconds)
        except TimeoutError:
            pass

    def set_phase(self, phase, request_id=None):
        self.phase, self.request_id = phase, request_id

    def collect(self, pid, ticks):
        return sample_memory(pid, ticks, self.phase, self.request_id, workers=self.workers)

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
                        "snapshot": periodic_environment(
                            environment_snapshot(constants=self.environment_constants)
                        ),
                    }
                    self.store.observation("environment.jsonl", observation)
                    environment_due = actual + 1
                self.store.flush_due()
                schedule["collector_work_ns"] = int((loop.time() - actual) * 1e9)
                self.store.observation("schedule.jsonl", schedule)
                due += interval
                if due < loop.time():
                    due += (int((loop.time() - due) // interval) + 1) * interval
                await self.wait(max(0, due - loop.time()))
        except Exception as exc:
            self.failure = exc
            raise
