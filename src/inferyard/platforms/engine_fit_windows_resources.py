"""Conservative Windows working-set and CPU sums from a stable live process tree."""

import math
import time

import psutil

from inferyard.config.engine_fit_native_sources import WINDOWS_SCOPE
from inferyard.platforms.engine_fit_windows import _remaining, _start
from inferyard.platforms.identity import PreflightError


def _row(pid, deadline):
    _remaining(deadline)
    start = _start(pid)
    process = psutil.Process(pid)
    parent = process.ppid()
    cpu, rss = process.cpu_times(), process.memory_info().rss
    children = [child.pid for child in process.children(recursive=False)]
    if (
        type(parent) is not int
        or parent < 0
        or type(rss) is not int
        or rss < 0
        or any(type(child) is not int or child < 1 for child in children)
        or len(children) != len(set(children))
        or any(
            type(value) not in (int, float) or not math.isfinite(value) or value < 0
            for value in (cpu.user, cpu.system)
        )
        or process.status() in (psutil.STATUS_ZOMBIE, psutil.STATUS_DEAD)
    ):
        raise PreflightError("engine_fit_process_counter_unavailable")
    if _start(pid) != start or process.ppid() != parent:
        raise PreflightError("engine_fit_process_tree_changed")
    _remaining(deadline)
    return {
        "start": start,
        "parent": parent,
        "cpu": cpu.user + cpu.system,
        "rss": rss,
        "children": set(children),
    }


def _tree(binding, deadline):
    if any(type(binding[key]) is not int or binding[key] < 1 for key in ("pid", "start_ticks")):
        raise PreflightError("engine_fit_service_identity_changed")
    rows, pending = {}, [(binding["pid"], None)]
    while pending:
        pid, parent = pending.pop()
        if pid in rows or len(rows) >= 256:
            raise PreflightError("engine_fit_process_tree_changed")
        row = _row(pid, deadline)
        if (parent is None and row["start"] != binding["start_ticks"]) or (
            parent is not None and row["parent"] != parent
        ):
            raise PreflightError("engine_fit_service_identity_changed")
        rows[pid] = row
        pending.extend((child, pid) for child in row["children"])
    for pid, row in rows.items():
        after = _row(pid, deadline)
        if any(after[key] != row[key] for key in ("start", "parent", "children")):
            raise PreflightError("engine_fit_process_tree_changed")
        if after["cpu"] < row["cpu"]:
            raise PreflightError("engine_fit_process_counter_unavailable")
    return rows


def resource_snapshot(binding):
    measures = ("process_tree_rss_bytes", "process_tree_cpu_seconds", "process_count")
    result = dict.fromkeys(("memory_available_bytes", *measures))
    result.update(scope=dict(WINDOWS_SCOPE), missing_reasons={})
    deadline = time.monotonic() + 10.0
    try:
        _remaining(deadline)
        memory = psutil.virtual_memory().available
        if type(memory) is not int or memory < 0:
            raise PreflightError("system_memory_unavailable")
        _remaining(deadline)
        result["memory_available_bytes"] = memory
    except (OSError, psutil.Error, PreflightError) as exc:
        result["missing_reasons"]["memory_available_bytes"] = (
            str(exc) if isinstance(exc, PreflightError) else "system_memory_unavailable"
        )
    try:
        rows = _tree(binding, deadline)
        cpu = sum(row["cpu"] for row in rows.values())
        if not math.isfinite(cpu):
            raise PreflightError("engine_fit_process_counter_unavailable")
        result.update(
            process_tree_rss_bytes=sum(row["rss"] for row in rows.values()),
            process_tree_cpu_seconds=cpu,
            process_count=len(rows),
        )
    except (OSError, psutil.Error, PreflightError) as exc:
        reason = str(exc) if isinstance(exc, PreflightError) else "engine_fit_resource_unreadable"
        result["missing_reasons"].update(dict.fromkeys(measures, reason))
    return result
