"""Enumerate TCP listeners without assuming executable names or unique owners."""

import ipaddress
import os
import platform
from pathlib import Path

from inferyard.platforms import identity


class DiscoveryError(RuntimeError):
    """Fixed collection error, without OS details or process arguments."""


def loopback_host(address):
    host = ipaddress.ip_address(address)
    if host.is_unspecified:
        return "::1" if host.version == 6 else "127.0.0.1"
    if host.is_loopback or (
        host.version == 6 and host.ipv4_mapped and host.ipv4_mapped.is_loopback
    ):
        return str(host)
    return None


def _process(pid, name, ticks, listeners):
    return {
        "pid": pid,
        "process_name": name,
        "process_start_ticks": ticks,
        "listeners": sorted(set(listeners)),
    }


def linux_listeners(proc_root=Path("/proc")):
    sockets, limitations, readable = {}, set(), 0
    for table, ipv6 in (("tcp", False), ("tcp6", True)):
        try:
            lines = (proc_root / "net" / table).read_text().splitlines()[1:]
            readable += 1
            for line in lines:
                try:
                    fields = line.split()
                    if fields[3] != "0A":
                        continue
                    encoded, port = fields[1].split(":")
                    host = loopback_host(identity._socket_address(encoded, ipv6))
                    port = int(port, 16)
                    if not 1 <= port <= 65535:
                        raise ValueError
                    if host is not None:
                        sockets[fields[9]] = (host, port)
                except (ValueError, IndexError):
                    limitations.add("listener_table_incomplete")
        except FileNotFoundError:
            if not ipv6:
                limitations.add("listener_table_incomplete")
        except (OSError, UnicodeError):
            limitations.add("listener_table_incomplete")
    if not readable:
        raise DiscoveryError("listener_enumeration_failed")
    if not sockets:
        return [], limitations
    try:
        directories = sorted(p for p in proc_root.iterdir() if p.name.isdecimal())
    except OSError as exc:
        raise DiscoveryError("listener_enumeration_failed") from exc
    processes, owned = [], set()
    for directory in directories:
        pid = int(directory.name)
        try:
            ticks = identity.process_start_ticks(pid, proc_root)
            inodes = set()
            for fd in (directory / "fd").iterdir():
                try:
                    target = os.readlink(fd)
                except FileNotFoundError:
                    limitations.add("listener_ownership_incomplete")
                    continue
                if target.startswith("socket:["):
                    inodes.add(target[8:-1])
            matched = inodes & sockets.keys()
            if not matched:
                continue
            name = Path(os.readlink(directory / "exe")).name
            if not name or identity.process_start_ticks(pid, proc_root) != ticks:
                limitations.add("process_identity_unavailable")
                continue
            processes.append(_process(pid, name, ticks, [sockets[i] for i in matched]))
            owned.update(matched)
        except (OSError, UnicodeError, identity.PreflightError):
            limitations.add("listener_ownership_incomplete")
    if sockets.keys() - owned:
        limitations.add("listener_ownership_incomplete")
    return processes, limitations


def psutil_listeners():
    import psutil

    processes, limitations = [], set()
    try:
        pids = psutil.pids()
    except (OSError, psutil.Error) as exc:
        raise DiscoveryError("listener_enumeration_failed") from exc
    for pid in pids:
        if pid <= 0:
            continue
        try:
            process = psutil.Process(pid)
            ticks = identity.process_start_ticks(pid)
            listeners = []
            for connection in process.net_connections(kind="tcp"):
                if connection.status != psutil.CONN_LISTEN:
                    continue
                host = loopback_host(connection.laddr.ip)
                if host is not None:
                    listeners.append((host, connection.laddr.port))
            if not listeners:
                continue
            name = Path(process.exe()).name
            if not name or identity.process_start_ticks(pid) != ticks:
                limitations.add("process_identity_unavailable")
                continue
            processes.append(_process(pid, name, ticks, listeners))
        except (OSError, psutil.Error, identity.PreflightError):
            limitations.add("listener_ownership_incomplete")
    return processes, limitations


def collect_listeners():
    system = platform.system()
    if system == "Linux":
        return linux_listeners()
    if system in ("Darwin", "Windows"):
        return psutil_listeners()
    raise DiscoveryError("service_discovery_unsupported_platform")
