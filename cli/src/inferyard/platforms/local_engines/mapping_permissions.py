"""Require kernel-reported non-executable mappings for unreadable data files."""

import re
import subprocess

from inferyard.platforms.identity import PreflightError

_REGION = re.compile(
    r"^(.*?)\s+([0-9a-fA-F]+)-([0-9a-fA-F]+)\s+\[[^\]]+\]\s+"
    r"([rwx-]{3})/([rwx-]{3})\s+SM=\S+\s+(/.*)$"
)


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
