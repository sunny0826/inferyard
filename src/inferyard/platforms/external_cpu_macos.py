"""Darwin native CPU counters, stored as integer microseconds, never Linux jiffies."""

import math

from inferyard.platforms.identity import PreflightError

SOURCE = "macos.psutil.host_minus_bound_pid.v1"
TICKS_PER_SECOND = 1_000_000


def ticks(value):
    if type(value) not in (int, float) or not math.isfinite(value) or value < 0:
        raise ValueError("invalid CPU counter")
    return int(value * TICKS_PER_SECOND)


def read_cpu(pid, start_ticks):
    from inferyard.platforms.macos_identity import process_start_ticks
    from inferyard.platforms.macos_native import psutil_module

    psutil = psutil_module()
    try:
        if process_start_ticks(pid) != start_ticks:
            raise PreflightError("source_changed")
        values = psutil.Process(pid).cpu_times()
        result = {"user_ticks": ticks(values.user), "system_ticks": ticks(values.system)}
        if process_start_ticks(pid) != start_ticks:
            raise PreflightError("source_changed")
        return result
    except psutil.AccessDenied as exc:
        raise PermissionError("permission_denied") from exc
    except psutil.Error as exc:
        raise PreflightError("source_unavailable") from exc


def capture(pid, start_ticks, boot, hz, phase, request, clock):
    from inferyard.platforms.macos_native import boot_id, psutil_module

    started = clock()
    host = service = None
    reason = None
    psutil = psutil_module()
    try:
        if not boot or boot_id() != boot:
            raise PreflightError("boot_identity_changed")
        if hz != TICKS_PER_SECOND or type(hz) is not int:
            raise ValueError("invalid CPU counter scale")
        values = psutil.cpu_times()
        host = [ticks(getattr(values, name)) for name in ("user", "nice", "system", "idle")]
        service = sum(read_cpu(pid, start_ticks).values())
        if boot_id() != boot:
            raise PreflightError("boot_identity_changed")
    except PermissionError, psutil.AccessDenied:
        reason = "permission_denied"
    except PreflightError as exc:
        reason = str(exc)
    except OSError, psutil.Error:
        reason = "source_unavailable"
    except ValueError, AttributeError, OverflowError:
        reason = "invalid_counter"
    if reason:
        host = service = None
    return {
        "source": SOURCE,
        "phase": phase,
        "request_id": request,
        "read_started_ns": started,
        "read_finished_ns": clock(),
        "server_pid": pid,
        "process_start_ticks": start_ticks,
        "boot_id": boot,
        "clock_ticks_per_second": hz,
        "host_ticks": host,
        "service_ticks": service,
        "missing_reason": reason,
    }


class ExternalCpu:
    """Safety guard with the same source validation as persisted observations."""

    def __init__(self, pid, start_ticks):
        from inferyard.platforms.macos_native import boot_id

        self.pid, self.start_ticks, self.boot = pid, start_ticks, boot_id()
        self.previous = None

    def sample(self):
        import time

        from inferyard.platforms.external_cpu import reduce_external_cpu

        row = capture(
            self.pid,
            self.start_ticks,
            self.boot,
            TICKS_PER_SECOND,
            "safety",
            None,
            time.monotonic_ns,
        )
        if row["missing_reason"]:
            raise PreflightError("external_cpu_source_unavailable:" + row["missing_reason"])
        previous, self.previous = self.previous, row
        if previous is None:
            return None
        result = reduce_external_cpu(
            [previous, row],
            {"server_pid": self.pid, "process_start_ticks": self.start_ticks},
            interval_ms=86_400_000,
        )["intervals"][-1]
        if result["missing_reason"]:
            raise PreflightError("external_cpu_counter_changed:" + result["missing_reason"])
        return result["value_percent"]
