"""Real local model-file ownership in a stable LM Studio service process tree."""

import os
import re
import time
from pathlib import Path

from inferyard.platforms.engine_fit_macos_listener import _records
from inferyard.platforms.engine_fit_observer_io import TOTAL_TIMEOUT, read_process, remaining
from inferyard.platforms.identity import PreflightError
from inferyard.platforms.macos_identity import process_start_ticks
from inferyard.platforms.macos_native import psutil_module

MAX_PROCESSES = 8


def lsof_records(pid, selectors, *, deadline):
    code, output, errors = read_process(
        ["/usr/sbin/lsof", "-nP", "-a", "-p", str(pid), *selectors, "-F0pftPDinT"],
        deadline=deadline,
        limit=1024 * 1024,
        prefix="engine_fit_lms_files",
        capture_stderr=True,
    )
    try:
        if code not in (0, 1) or errors.strip() or (code == 1 and output.strip()):
            raise ValueError
        return _records(output.decode("utf-8"))
    except (UnicodeError, ValueError) as exc:
        raise PreflightError("engine_fit_lms_model_file_unavailable") from exc


def _identity(pid, psutil):
    start = process_start_ticks(pid)
    process = psutil.Process(pid)
    parent, uids = process.ppid(), tuple(process.uids())
    children = [child.pid for child in process.children(recursive=False)]
    if (
        type(parent) is not int
        or parent < 0
        or len(uids) != 3
        or any(type(uid) is not int or uid != os.getuid() for uid in uids)
        or any(type(child) is not int or child < 1 for child in children)
        or len(set(children)) != len(children)
        or process.status() in (psutil.STATUS_ZOMBIE, psutil.STATUS_DEAD)
    ):
        raise PreflightError("engine_fit_lms_file_owner_unverified")
    if process_start_ticks(pid) != start or process.ppid() != parent:
        raise PreflightError("engine_fit_lms_file_owner_changed")
    return start, parent, uids, frozenset(children)


def _tree(pid, start, psutil, deadline):
    result, pending = {}, [(pid, None)]
    while pending:
        remaining(deadline)
        current, parent = pending.pop()
        if current in result or len(result) >= MAX_PROCESSES:
            raise PreflightError("engine_fit_lms_file_owner_unverified")
        row = _identity(current, psutil)
        if (parent is None and row[0] != start) or (parent is not None and row[1] != parent):
            raise PreflightError("engine_fit_lms_file_owner_changed")
        result[current] = row
        pending.extend((child, current) for child in row[3])
    return result


def _matches(row, pid, stat):
    try:
        descriptor = row.get("f")
        return (
            type(row.get("p")) is int
            and row["p"] == pid
            and row.get("t") == "REG"
            and isinstance(descriptor, str)
            and (descriptor == "txt" or re.fullmatch(r"[0-9]+", descriptor) is not None)
            and int(row.get("D", ""), 16) == stat.st_dev
            and int(row.get("i", "")) == stat.st_ino
        )
    except ValueError, TypeError:
        return False


def verify_model_file(pid, start, model, *, psutil=None, deadline=None):
    """Require an observed mapping/FD, never only a model-root path declaration."""
    from inferyard.platforms.engine_fit import _stamp

    psutil = psutil or psutil_module()
    deadline = deadline if deadline is not None else time.monotonic() + TOTAL_TIMEOUT
    try:
        model = Path(model)
        before = model.stat()
        tree = _tree(pid, start, psutil, deadline)
        found = False
        for owner in tree:
            remaining(deadline)
            rows = lsof_records(owner, [], deadline=deadline)
            for row in rows:
                if _matches(row, owner, before):
                    found = True
                elif _additional_gguf(row, owner):
                    raise PreflightError("engine_fit_lms_additional_gguf_observed")
        if _tree(pid, start, psutil, deadline) != tree or _stamp(model.stat()) != _stamp(before):
            raise PreflightError("engine_fit_lms_file_owner_changed")
        if not found:
            raise PreflightError("engine_fit_lms_model_file_not_observed")
    except (OSError, ValueError, psutil.Error) as exc:
        raise PreflightError("engine_fit_lms_model_file_unavailable") from exc


def _additional_gguf(row, pid):
    descriptor, name = row.get("f"), row.get("n")
    return (
        type(row.get("p")) is int
        and row["p"] == pid
        and row.get("t") == "REG"
        and isinstance(descriptor, str)
        and (descriptor == "txt" or re.fullmatch(r"[0-9]+", descriptor) is not None)
        and isinstance(name, str)
        and name.lower().endswith(".gguf")
    )
