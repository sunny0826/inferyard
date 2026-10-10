"""Small, bounded macOS native queries shared by identity and resource collectors."""

import math
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


SWAP_IN_SOURCE = "host_statistics64:swapins"
SWAP_OUT_SOURCE = "host_statistics64:swapouts"
_HOST_VM_INFO64 = 4


def _host_vm_swap():
    """One host_statistics64 read of Swapins/Swapouts. Pageins are not swap."""
    import ctypes

    class _VmStatistics64(ctypes.Structure):
        _fields_ = [
            ("free_count", ctypes.c_uint),
            ("active_count", ctypes.c_uint),
            ("inactive_count", ctypes.c_uint),
            ("wire_count", ctypes.c_uint),
            ("zero_fill_count", ctypes.c_uint64),
            ("reactivations", ctypes.c_uint64),
            ("pageins", ctypes.c_uint64),
            ("pageouts", ctypes.c_uint64),
            ("faults", ctypes.c_uint64),
            ("cow_faults", ctypes.c_uint64),
            ("lookups", ctypes.c_uint64),
            ("hits", ctypes.c_uint64),
            ("purges", ctypes.c_uint64),
            ("purgeable_count", ctypes.c_uint),
            ("speculative_count", ctypes.c_uint),
            ("decompressions", ctypes.c_uint64),
            ("compressions", ctypes.c_uint64),
            ("swapins", ctypes.c_uint64),
            ("swapouts", ctypes.c_uint64),
            ("compressor_page_count", ctypes.c_uint),
            ("throttled_count", ctypes.c_uint),
            ("external_page_count", ctypes.c_uint),
            ("internal_page_count", ctypes.c_uint),
            ("total_uncompressed_pages_in_compressor", ctypes.c_uint64),
        ]

    libc = ctypes.CDLL(None)
    host = libc.mach_host_self
    host.argtypes = []
    host.restype = ctypes.c_uint
    stats_call = libc.host_statistics64
    stats_call.argtypes = [
        ctypes.c_uint,
        ctypes.c_int,
        ctypes.c_void_p,
        ctypes.POINTER(ctypes.c_uint),
    ]
    stats_call.restype = ctypes.c_int
    page_call = libc.host_page_size
    page_call.argtypes = [ctypes.c_uint, ctypes.POINTER(ctypes.c_size_t)]
    page_call.restype = ctypes.c_int
    host_port = host()
    stats = _VmStatistics64()
    count = ctypes.c_uint(ctypes.sizeof(stats) // ctypes.sizeof(ctypes.c_uint))
    try:
        if stats_call(host_port, _HOST_VM_INFO64, ctypes.byref(stats), ctypes.byref(count)) != 0:
            return None, None, None
        needed = (_VmStatistics64.swapouts.offset + ctypes.sizeof(ctypes.c_uint64)) // 4
        if count.value < needed:
            return None, None, None
        page = ctypes.c_size_t()
        if page_call(host_port, ctypes.byref(page)) != 0 or page.value <= 0:
            return None, None, None
        return int(page.value), int(stats.swapins), int(stats.swapouts)
    finally:
        _release_host_port(libc, host_port)


def _release_host_port(libc, host_port):
    import ctypes

    task = ctypes.c_uint.in_dll(libc, "mach_task_self_").value
    release = libc.mach_port_deallocate
    release.argtypes = [ctypes.c_uint, ctypes.c_uint]
    release.restype = ctypes.c_int
    release(task, host_port)


def swap_snapshot():
    sources = {"pswpin": SWAP_IN_SOURCE, "pswpout": SWAP_OUT_SOURCE}
    result = {"pswpin": None, "pswpout": None, "page_size_bytes": None, "source": sources}
    try:
        page_size, swapins, swapouts = _host_vm_swap()
    except AttributeError, OSError, ValueError:
        return result
    if type(page_size) is not int or page_size <= 0:
        return result
    result["page_size_bytes"] = page_size
    for key, value in (("pswpin", swapins), ("pswpout", swapouts)):
        if type(value) is int and value >= 0:
            result[key] = value
    return result
