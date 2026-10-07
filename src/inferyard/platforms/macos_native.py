"""Small, bounded macOS native queries shared by identity and resource collectors."""

import math
import re
import subprocess
import uuid


def psutil_module():
    import psutil

    return psutil


def query(arguments, *, timeout=2):
    try:
        result = subprocess.run(arguments, capture_output=True, text=True, timeout=timeout)
        return result.stdout.strip() if result.returncode == 0 else None
    except OSError, UnicodeError, subprocess.TimeoutExpired:
        return None


def sysctl_text(key):
    return query(["/usr/sbin/sysctl", "-n", key])


def sysctl_string(key):
    """Read one bounded C string afresh; integer sysctl values are not text."""
    import ctypes

    if not isinstance(key, str) or not key or "\0" in key:
        return None
    try:
        name = key.encode("ascii")
        libc = ctypes.CDLL(None, use_errno=True)
        call = libc.sysctlbyname
        call.argtypes = [
            ctypes.c_char_p,
            ctypes.c_void_p,
            ctypes.POINTER(ctypes.c_size_t),
            ctypes.c_void_p,
            ctypes.c_size_t,
        ]
        call.restype = ctypes.c_int
        buffer = ctypes.create_string_buffer(4096)
        size = ctypes.c_size_t(len(buffer))
        if call(name, buffer, ctypes.byref(size), None, 0) != 0:
            return None
        if not 0 < size.value <= len(buffer):
            return None
        raw = buffer.raw[: size.value]
        if not raw.endswith(b"\0") or b"\0" in raw[:-1]:
            return None
        return raw[:-1].decode("utf-8").strip() or None
    except AttributeError, OSError, UnicodeError, ValueError:
        return None


def boot_id():
    # A process spawn per CPU/swap boundary would itself disturb short windows.
    # sysctlbyname is the same native source, read afresh without a subprocess.
    import ctypes

    try:
        libc = ctypes.CDLL(None, use_errno=True)
        call = libc.sysctlbyname
        call.argtypes = [
            ctypes.c_char_p,
            ctypes.c_void_p,
            ctypes.POINTER(ctypes.c_size_t),
            ctypes.c_void_p,
            ctypes.c_size_t,
        ]
        call.restype = ctypes.c_int
        size = ctypes.c_size_t(128)
        buffer = ctypes.create_string_buffer(size.value)
        if call(b"kern.bootsessionuuid", buffer, ctypes.byref(size), None, 0) != 0:
            return None
        if not 0 < size.value <= len(buffer):
            return None
        value = buffer.raw[: size.value].rstrip(b"\0").decode("ascii")
    except AttributeError, OSError, UnicodeError:
        return None
    try:
        return str(uuid.UUID(value)) if value else None
    except ValueError:
        return None


def microseconds(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        raise ValueError("invalid_native_time")
    if not math.isfinite(value) or value < 0:
        raise ValueError("invalid_native_time")
    return round(value * 1_000_000)


def swap_snapshot():
    sources = {
        "pswpin": "vm_stat:Swapins",
        "pswpout": "vm_stat:Swapouts",
    }
    result = {"pswpin": None, "pswpout": None, "page_size_bytes": None, "source": sources}
    # psutil 7.2 Darwin sin/sout expose pageins/pageouts, including file paging.
    # Only vm_stat's separate Swapins/Swapouts counters represent actual swap.
    text = query(["/usr/bin/vm_stat"])
    header = re.search(r"page size of ([1-9][0-9]*) bytes", text or "")
    if not header:
        return result
    result["page_size_bytes"] = int(header[1])
    for key, label in (("pswpin", "Swapins"), ("pswpout", "Swapouts")):
        values = re.findall(rf"^{label}:\s*([0-9]+)\.\s*$", text, re.MULTILINE)
        if len(values) == 1:
            result[key] = int(values[0])
    return result
