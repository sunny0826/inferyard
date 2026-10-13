"""Bounded, isolated macOS process enumeration; no arguments or environments are read."""

import json
import os
import plistlib
import selectors
import signal
import subprocess
import sys
import time
from pathlib import Path

from inferyard.platforms.identity import PreflightError, process_start_ticks

IDENTITY_SOURCE = "psutil:macos:raw_kernel_starttime:microseconds"
RSS_SOURCE = "psutil:Process.memory_info:rss_bytes"


def reading(value, source, reason=None):
    return {"value": value, "source": source, "missing_reason": reason}


def bundle_identity(executable):
    if not executable:
        return None
    try:
        executable = Path(executable).resolve(strict=True)
        candidates = [p for p in executable.parents if p.suffix == ".app"]
        if not candidates:
            return None
        bundle = candidates[-1]
        metadata = bundle / "Contents/Info.plist"
        if metadata.stat().st_size > 1024 * 1024:
            return None
        with metadata.open("rb") as handle:
            info = plistlib.load(handle)
        if not isinstance(info, dict) or info.get("CFBundlePackageType") != "APPL":
            return None
        stamp = bundle.stat()
        return {
            "key": f"{bundle}:{stamp.st_dev}:{stamp.st_ino}",
            "name": bundle.stem,
            "path": str(bundle),
        }
    except OSError, ValueError, plistlib.InvalidFileException:
        return None


def _parent_identity(pid):
    if not pid:
        return None, None
    try:
        return process_start_ticks(pid), None
    except PreflightError, OSError, ValueError:
        return None, "parent_identity_unavailable"


def observe(pid, psutil):
    row = {
        "pid": pid,
        "start_identity": None,
        "parent_pid": 0,
        "parent_start_identity": None,
        "parent_identity_missing_reason": None,
        "name": "unavailable",
        "uid": None,
        "executable": None,
        "bundle": None,
        "rss": reading(None, RSS_SOURCE, "unavailable"),
        "missing_reason": None,
    }
    try:
        process = psutil.Process(pid)
        row["uid"] = process.uids().real
        row["start_identity"] = process_start_ticks(pid)
        row["uid"] = process.uids().real
        row["parent_pid"] = process.ppid()
        row["name"] = process.name() or "unavailable"
        row["executable"] = process.exe() or None
        row["parent_start_identity"], row["parent_identity_missing_reason"] = _parent_identity(
            row["parent_pid"]
        )
        rss = process.memory_info().rss
        if type(rss) is not int or rss < 0:
            raise ValueError
        row["rss"] = reading(rss, RSS_SOURCE)
        row["bundle"] = bundle_identity(row["executable"])
        final_parent_pid = process.ppid()
        final_parent_start, final_parent_reason = _parent_identity(final_parent_pid)
        if final_parent_pid != row["parent_pid"]:
            row["parent_pid"] = final_parent_pid
            row["parent_start_identity"] = None
            row["parent_identity_missing_reason"] = "parent_identity_changed"
        elif row["parent_identity_missing_reason"] or final_parent_reason:
            row["parent_start_identity"] = None
            row["parent_identity_missing_reason"] = (
                row["parent_identity_missing_reason"] or final_parent_reason
            )
        elif row["parent_start_identity"] != final_parent_start:
            row["parent_start_identity"] = None
            row["parent_identity_missing_reason"] = "parent_identity_changed"
        if row["start_identity"] != process_start_ticks(pid) or row["uid"] != process.uids().real:
            row["missing_reason"] = "identity_changed"
    except psutil.AccessDenied, PermissionError:
        row["missing_reason"] = "access_denied"
    except psutil.NoSuchProcess, psutil.ZombieProcess:
        row["missing_reason"] = "process_exited"
    except PreflightError, psutil.Error, OSError, ValueError:
        row["missing_reason"] = "identity_or_query_unavailable"
    if row["missing_reason"]:
        row["rss"] = reading(None, RSS_SOURCE, row["missing_reason"])
        row["bundle"] = None
        # Inconsistent identity must never establish a parent edge or protection request.
        row["start_identity"] = None
        row["parent_start_identity"] = None
        row["parent_identity_missing_reason"] = None
    return row


def _worker(connection, max_processes):
    try:
        import psutil

        memory = psutil.virtual_memory()
        pids = psutil.pids()
        connection.send(
            (
                "host",
                {
                    "memory_total": reading(memory.total, "psutil:virtual_memory:total_bytes"),
                    "memory_available": reading(
                        memory.available, "psutil:virtual_memory:available_bytes"
                    ),
                    "total_pids": len(pids),
                },
            )
        )
        for pid in pids[:max_processes]:
            connection.send(("process", observe(pid, psutil)))
        connection.send(("done", "process_limit" if len(pids) > max_processes else None))
    except Exception:
        # The parent maps failure without persisting exception text or local paths.
        connection.send(("error", None))
    finally:
        connection.close()


def collect(*, deadline_seconds, max_processes, command=None):
    started = time.monotonic_ns()
    deadline = time.monotonic() + deadline_seconds
    rows, host, truncation = [], None, None
    process = subprocess.Popen(
        command or [sys.executable, "-m", __name__, str(max_processes)],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
    )
    buffer = b""
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            done = False
            while not done:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    truncation = "deadline"
                    break
                if not selector.select(min(remaining, 0.05)):
                    continue
                chunk = os.read(process.stdout.fileno(), 65536)
                if not chunk:
                    raise OSError("memory_collector_failed")
                buffer += chunk
                while b"\n" in buffer:
                    line, buffer = buffer.split(b"\n", 1)
                    tag, data = json.loads(line)
                    if tag == "host":
                        host = data
                    elif tag == "process":
                        rows.append(data)
                    elif tag == "done":
                        truncation = data
                        done = True
                        break
                    else:
                        raise OSError("memory_collector_failed")
    finally:
        process.stdout.close()
        try:
            process.wait(timeout=0.1)
        except subprocess.TimeoutExpired:
            if process.poll() is None:
                process.terminate()
            try:
                process.wait(timeout=0.2)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=0.2)
    if host is None:
        host = {
            "memory_total": reading(None, "psutil:virtual_memory:total_bytes", "deadline"),
            "memory_available": reading(None, "psutil:virtual_memory:available_bytes", "deadline"),
            "total_pids": len(rows),
        }
    return {
        **host,
        "rows": rows,
        "truncation_reason": truncation,
        "collection_ns": time.monotonic_ns() - started,
        "caller_pid": os.getpid(),
    }


class _JsonLines:
    def send(self, message):
        print(json.dumps(message, ensure_ascii=True, allow_nan=False), flush=True)

    def close(self):
        pass


if __name__ == "__main__":
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    _worker(_JsonLines(), int(sys.argv[1]))
