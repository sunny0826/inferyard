"""Require kernel-reported non-executable mappings for unreadable data files."""

import os
import re
import subprocess
from pathlib import Path

from inferyard.errors import PreflightError
from inferyard.platforms.identity import process_start_ticks


_REGION = re.compile(
    r"^(.*?)\s+([0-9a-fA-F]+)-([0-9a-fA-F]+)\s+\[[^\]]+\]\s+"
    r"([rwx-]{3})/([rwx-]{3})\s+SM=\S+\s+(/.*)$"
)
_CACHE_LIMIT = 256
_OS_UNREADABLE_DATA_PREFIXES = ("/private/var/db/", "/var/db/")
_NON_EXEC_CACHE: dict[tuple[int, int, tuple[int, int], str], None] = {}


def _os_unreadable_data(path: Path | str) -> bool:
    """Root-owned OS data that is unreadable or already unlinked (e.g. logd plist caches).

    Only these paths may reuse a prior vmmap verdict; user paths are always re-observed.
    """
    if not str(path).startswith(_OS_UNREADABLE_DATA_PREFIXES):
        return False
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NONBLOCK | os.O_NOFOLLOW)
    except PermissionError, FileNotFoundError:
        return True
    except OSError:
        return False
    os.close(fd)
    return False


def cached_non_executable(pid: int, stamp: tuple[int, int], path: Path | str) -> bool:
    try:
        start_ticks = process_start_ticks(pid)
    except PreflightError:
        return False
    key = (pid, start_ticks, stamp, str(path))
    if key not in _NON_EXEC_CACHE or not _os_unreadable_data(path):
        return False
    _NON_EXEC_CACHE.pop(key, None)
    _NON_EXEC_CACHE[key] = None
    return True


def remember_non_executable(pid: int, stamp: tuple[int, int], path: Path | str) -> None:
    if not _os_unreadable_data(path):
        return
    try:
        start_ticks = process_start_ticks(pid)
    except PreflightError:
        return
    key = (pid, start_ticks, stamp, str(path))
    _NON_EXEC_CACHE.pop(key, None)
    _NON_EXEC_CACHE[key] = None
    while len(_NON_EXEC_CACHE) > _CACHE_LIMIT:
        _NON_EXEC_CACHE.pop(next(iter(_NON_EXEC_CACHE)))


def observe(pid):
    try:
        result = subprocess.run(
            ["/usr/bin/vmmap", "-w", "-interleaved", str(pid)],
            capture_output=True,
            text=True,
            timeout=5,
        )
        if result.returncode or len(result.stdout) > 8 * 1024**2:
            raise PreflightError("local_engine_mapping_unverified")
        return result.stdout
    except (OSError, UnicodeError, subprocess.TimeoutExpired) as exc:
        raise PreflightError("local_engine_mapping_unverified") from exc


def require_non_executable(text, path):
    suffix = " " + str(path)
    lines = [line for line in text.splitlines() if line.endswith(suffix)]
    if not lines:
        raise PreflightError("local_engine_mapping_unverified")
    for line in lines:
        match = _REGION.fullmatch(line)
        if (
            match is None
            or match[1].strip() != "mapped file"
            or int(match[2], 16) >= int(match[3], 16)
            or "x" in match[4] + match[5]
            or match[6] != str(path)
        ):
            raise PreflightError("local_engine_mapping_unverified")
