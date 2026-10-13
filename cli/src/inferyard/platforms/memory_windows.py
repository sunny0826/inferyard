"""Bounded Windows inventory. No command lines, environments or service actions."""

import json
import os
import signal
import subprocess
import sys
import time
from pathlib import Path

from inferyard.platforms.memory_windows_api import owner_session
from inferyard.platforms.windows_api import process_start

IDENTITY_SOURCE = "GetProcessTimes:creation_FILETIME_100ns_since_1601"
RSS_SOURCE = "psutil:Windows:WorkingSetSize:bytes"
PRIVATE_SOURCE = "psutil:Windows:PrivateUsage:bytes"


def reading(value, source, reason=None):
    return {"value": value, "source": source, "missing_reason": reason}


def _number(value):
    if type(value) is not int or value < 0:
        raise ValueError("invalid_measurement")
    return value


def _parent(pid):
    if pid == 0:
        return None, None
    try:
        return process_start(pid), None
    except OSError:
        return None, "parent_identity_unavailable"


def _executable(path):
    if not path:
        return None
    try:
        resolved = Path(path).resolve(strict=True)
        stamp = resolved.stat()
        if not stamp.st_ino or not resolved.is_file():
            return None
        return {
            "key": f"{str(resolved).casefold()}:{stamp.st_dev}:{stamp.st_ino}",
            "name": resolved.name,
            "path": str(resolved),
        }
    except OSError:
        return None


def observe(pid, psutil):
    row = {
        "pid": pid,
        "start_identity": None,
        "parent_pid": 0,
        "parent_start_identity": None,
        "parent_identity_missing_reason": None,
        "name": "unavailable",
        "owner": None,
        "session": None,
        "ownership_missing_reason": "ownership_or_session_unavailable",
        "executable": None,
        "image": None,
        "rss": reading(None, RSS_SOURCE, "unavailable"),
        "private_commit": reading(None, PRIVATE_SOURCE, "unavailable"),
        "missing_reason": None,
    }
    try:
        row["start_identity"] = process_start(pid)
        process = psutil.Process(pid)
        row["name"] = (process.name() or "unavailable")[:128]
        row["parent_pid"] = process.ppid()
        row["parent_start_identity"], row["parent_identity_missing_reason"] = _parent(
            row["parent_pid"]
        )
        try:
            row["owner"], row["session"] = owner_session(pid)
            row["ownership_missing_reason"] = None
        except OSError:
            pass
        try:
            row["executable"] = process.exe() or None
            row["image"] = _executable(row["executable"])
        except psutil.Error, OSError:
            pass
        info = process.memory_info()
        row["rss"] = reading(_number(info.rss), RSS_SOURCE)
        try:
            row["private_commit"] = reading(_number(info.private), PRIVATE_SOURCE)
        except AttributeError, ValueError:
            row["private_commit"] = reading(None, PRIVATE_SOURCE, "private_commit_unavailable")
        final_parent = process.ppid()
        final_start, final_reason = _parent(final_parent)
        if final_parent != row["parent_pid"] or (
            final_start is not None
            and row["parent_start_identity"] is not None
            and (final_start != row["parent_start_identity"] or final_start > row["start_identity"])
        ):
            row["parent_pid"] = final_parent
            row["parent_start_identity"] = None
            row["parent_identity_missing_reason"] = "parent_identity_changed"
        elif final_reason or row["parent_identity_missing_reason"]:
            row["parent_start_identity"] = None
            row["parent_identity_missing_reason"] = "parent_identity_unavailable"
        if row["owner"] is not None:
            try:
                if (row["owner"], row["session"]) != owner_session(pid):
                    row["missing_reason"] = "identity_changed"
            except OSError:
                row["owner"], row["session"] = None, None
                row["ownership_missing_reason"] = "ownership_or_session_unavailable"
        if row["start_identity"] != process_start(pid):
            row["missing_reason"] = "identity_changed"
    except psutil.NoSuchProcess, psutil.ZombieProcess:
        row["missing_reason"] = "process_exited"
    except psutil.AccessDenied, PermissionError:
        row["missing_reason"] = "access_denied"
    except OSError as exc:
        row["missing_reason"] = (
            "system_identity_unavailable"
            if pid in (0, 4)
            else "access_denied"
            if getattr(exc, "winerror", None) == 5
            else (
                "process_exited"
                if getattr(exc, "winerror", None) == 87
                else "identity_or_query_unavailable"
            )
        )
    except psutil.Error, ValueError:
        row["missing_reason"] = "identity_or_query_unavailable"
    if row["missing_reason"]:
        row["start_identity"] = row["parent_start_identity"] = None
        row["parent_identity_missing_reason"] = None
        row["image"] = None
        row["rss"] = reading(None, RSS_SOURCE, row["missing_reason"])
        row["private_commit"] = reading(None, PRIVATE_SOURCE, row["missing_reason"])
        row["owner"] = row["session"] = None
        row["ownership_missing_reason"] = "ownership_or_session_unavailable"
    return row


def _send(tag, data):
    print(json.dumps((tag, data), ensure_ascii=True, allow_nan=False), flush=True)


def _worker(max_processes):
    import psutil

    try:
        try:
            memory = psutil.virtual_memory()
            total = reading(_number(memory.total), "GlobalMemoryStatusEx:ullTotalPhys")
            available = reading(_number(memory.available), "GlobalMemoryStatusEx:ullAvailPhys")
        except OSError, psutil.Error, ValueError:
            total = reading(None, "GlobalMemoryStatusEx:ullTotalPhys", "host_memory_unavailable")
            available = reading(
                None, "GlobalMemoryStatusEx:ullAvailPhys", "host_memory_unavailable"
            )
        pids = sorted(set(psutil.pids()))
        try:
            caller_owner, caller_session = owner_session(os.getppid())
        except OSError:
            caller_owner, caller_session = None, None
        _send(
            "host",
            {
                "total_pids": len(pids),
                "memory_total": total,
                "memory_available": available,
                "caller_owner": caller_owner,
                "caller_session": caller_session,
                "collector_pid": os.getpid(),
                "system_root": os.environ.get("SystemRoot"),
            },
        )
        for pid in pids[:max_processes]:
            _send("process", observe(pid, psutil))
        _send("done", "process_limit" if len(pids) > max_processes else None)
    except Exception:
        _send("error", None)


def _worker_command(max_processes):
    # Windows venv python.exe is a redirector that can outlive/be outlived by its
    # interpreter child. Own the actual interpreter handle so timeout/cancel
    # cannot leave the real collector running. Preserve the current environment's
    # imports (including editable installs) without shell expansion or activation.
    bootstrap = (
        f"import runpy, sys; sys.path = {sys.path!r}; "
        f"sys.argv = [{__name__!r}, {str(max_processes)!r}]; "
        f"runpy.run_module({__name__!r}, run_name='__main__')"
    )
    return [getattr(sys, "_base_executable", sys.executable), "-c", bootstrap]


def collect(*, deadline_seconds, max_processes, command=None):
    started = time.monotonic_ns()
    process = subprocess.Popen(
        command or _worker_command(max_processes),
        stdin=subprocess.DEVNULL,
        stdout=subprocess.PIPE,
        stderr=subprocess.DEVNULL,
    )
    timed_out = False
    try:
        remaining = max(0, deadline_seconds - (time.monotonic_ns() - started) / 1e9)
        try:
            output, _ = process.communicate(timeout=remaining)
        except subprocess.TimeoutExpired:
            timed_out = True
            process.kill()  # Only this owned inventory worker, never an observed PID.
            output, _ = process.communicate(timeout=0.5)
    finally:
        if process.poll() is None:
            process.kill()
            process.wait(timeout=0.5)
        process.stdout.close()
    host, rows, done, truncation = None, [], False, "deadline" if timed_out else None
    # Timeout can leave a partial final JSON line; only complete records are evidence.
    for line in output.splitlines(keepends=True):
        if not line.endswith(b"\n") and timed_out:
            break
        tag, data = json.loads(line)
        if tag == "host" and host is None:
            host = data
        elif tag == "process" and len(rows) < max_processes:
            rows.append(data)
        elif tag == "done":
            done = True
            if not timed_out:
                truncation = data
        else:
            raise OSError("memory_collector_failed")
    if not timed_out and (process.returncode != 0 or not done or host is None):
        raise OSError("memory_collector_failed")
    if host is None:
        host = {
            "total_pids": 0,
            "caller_owner": None,
            "caller_session": None,
            "collector_pid": process.pid,
            "system_root": None,
            "memory_total": reading(None, "GlobalMemoryStatusEx:ullTotalPhys", "deadline"),
            "memory_available": reading(None, "GlobalMemoryStatusEx:ullAvailPhys", "deadline"),
        }
    return {
        **host,
        "rows": rows,
        "truncation_reason": truncation,
        "collection_ns": time.monotonic_ns() - started,
        "caller_pid": os.getpid(),
    }


if __name__ == "__main__":
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    _worker(int(sys.argv[1]))
