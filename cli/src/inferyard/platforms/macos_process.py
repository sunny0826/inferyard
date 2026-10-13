"""Read-only lsof ownership and file mapping evidence, without shell execution."""

import ipaddress
import subprocess

from inferyard.platforms.identity import PreflightError


def lsof_records(pid, selectors):
    try:
        result = subprocess.run(
            ["/usr/sbin/lsof", "-nP", "-a", "-p", str(pid), *selectors, "-F0pftPDinT"],
            capture_output=True,
            text=True,
            timeout=2,
        )
        if result.returncode not in (0, 1) or result.stderr.strip():
            raise PreflightError("listener_identity_unavailable")
        if result.returncode == 1 and result.stdout.strip():
            raise PreflightError("listener_identity_unavailable")
        records, current, owner = [], None, None
        for field in result.stdout.split("\0"):
            field = field.lstrip("\n")
            if not field:
                continue
            key, value = field[0], field[1:]
            if key in ("p", "f") and current is not None:
                records.append(current)
                current = None
            if key == "p":
                owner = int(value)
            elif key == "f":
                if owner is None:
                    raise ValueError
                current = {"p": owner, "f": value}
            elif current is not None:
                if key == "T" and not value.startswith("ST="):
                    continue
                if key in current:
                    raise ValueError
                current[key] = value
        if current is not None:
            records.append(current)
        return records
    except (OSError, UnicodeError, ValueError, subprocess.TimeoutExpired) as exc:
        raise PreflightError("listener_identity_unavailable") from exc


def matching_listener(row, address, port):
    try:
        target = ipaddress.ip_address(address)
        if row.get("P") != "TCP" or row.get("T") != "ST=LISTEN":
            return False
        if row.get("t") != ("IPv6" if target.version == 6 else "IPv4"):
            return False
        host, separator, raw_port = row.get("n", "").rpartition(":")
        if not separator or not raw_port.isdecimal() or int(raw_port) != port:
            return False
        host = host.removeprefix("[").removesuffix("]")
        if host == "*":
            return True
        local = ipaddress.ip_address(host)
        return local.version == target.version and (local == target or local.is_unspecified)
    except ValueError:
        return False


def mapped_file(pid, identity, rows=None):
    """Match a text mapping in a same-check view, or perform an independent read."""
    try:
        return any(
            row.get("p") == pid
            and row.get("f") == "txt"
            and row.get("t") == "REG"
            and int(row.get("D", ""), 16) == identity.device
            and int(row.get("i", "")) == identity.inode
            for row in (rows if rows is not None else lsof_records(pid, ["-d", "txt"]))
        )
    except ValueError:
        return False
