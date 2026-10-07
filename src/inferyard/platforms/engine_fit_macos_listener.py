"""Reject shared TCP listeners that could route requests to an unbound service."""

import subprocess

from inferyard.platforms.identity import PreflightError
from inferyard.platforms.macos_process import matching_listener


def _records(output):
    records, current, owner = [], None, None
    for field in output.split("\0"):
        field = field.lstrip("\n")
        if not field:
            continue
        key, value = field[0], field[1:]
        if key in ("p", "f") and current is not None:
            records.append(current)
            current = None
        if key == "p":
            owner = int(value)
            if owner < 1:
                raise ValueError
        elif key == "f":
            if owner is None or not value:
                raise ValueError
            current = {"p": owner, "f": value}
        elif current is not None:
            if key == "T" and not value.startswith("ST="):
                continue
            if key in current:
                raise ValueError
            current[key] = value
        else:
            raise ValueError
    if current is not None:
        records.append(current)
    return records


def unique_listener(pid, address, port, *, timeout=2):
    """Require one globally observed listening FD; do not restrict the query by PID."""
    try:
        result = subprocess.run(
            [
                "/usr/sbin/lsof",
                "-nP",
                "-a",
                f"-iTCP:{port}",
                "-sTCP:LISTEN",
                "-F0pftPnT",
            ],
            capture_output=True,
            text=True,
            timeout=timeout,
        )
        if result.returncode not in (0, 1) or result.stderr.strip():
            raise ValueError
        if result.returncode == 1 and result.stdout.strip():
            raise ValueError
        records = _records(result.stdout)
        # A second family or bound address is deliberately ambiguous: lsof does
        # not expose whether an IPv6 socket also accepts the IPv4 destination.
        if (
            len(records) != 1
            or records[0]["p"] != pid
            or not matching_listener(records[0], address, port)
        ):
            raise PreflightError("engine_fit_listener_ambiguous_or_changed")
    except (OSError, UnicodeError, ValueError, subprocess.TimeoutExpired) as exc:
        raise PreflightError("engine_fit_listener_identity_unavailable") from exc
