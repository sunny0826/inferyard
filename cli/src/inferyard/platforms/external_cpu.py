"""Raw host/process CPU evidence; residual busy time is not per-process attribution."""

import os
import sys
from pathlib import Path

from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.identity import PreflightError

SOURCE = "linux.proc.host_minus_bound_pid.v1"
MACOS_SOURCE = "macos.psutil.host_minus_bound_pid.v1"


def native_macos(root=Path("/proc")):
    return sys.platform == "darwin" and root == Path("/proc")


def clock_ticks_per_second(root=Path("/proc")):
    if native_macos(root):
        return 1_000_000
    return os.sysconf("SC_CLK_TCK") if hasattr(os, "sysconf") else None


def read_boot_id(root=Path("/proc")):
    if native_macos(root):
        from inferyard.platforms.macos_native import boot_id

        value = boot_id()
    else:
        value = (root / "sys/kernel/random/boot_id").read_text().strip()
    if not isinstance(value, str) or not value or len(value) > 128:
        raise ValueError("boot identity unavailable")
    return value


def capture(pid, ticks, boot, hz, phase, request, clock, root=Path("/proc")):
    if native_macos(root):
        from inferyard.platforms.external_cpu_macos import capture as capture_macos

        return capture_macos(pid, ticks, boot, hz, phase, request, clock)
    from inferyard.platforms.resources_linux import read_cpu

    started = clock()
    host = service = None
    reason = None
    try:
        if not boot or (root / "sys/kernel/random/boot_id").read_text().strip() != boot:
            raise PreflightError("boot_identity_changed")
        fields = (root / "stat").read_text().splitlines()[0].split()
        if len(fields) < 9 or fields[0] != "cpu":
            raise ValueError("invalid host CPU counters")
        host = [int(v) for v in fields[1:9]]
        if min(host) < 0 or type(hz) is not int or hz <= 0:
            raise ValueError("invalid CPU counter scale")
        values = read_cpu(pid, ticks, root)
        service = values["user_ticks"] + values["system_ticks"]
    except OSError, ValueError, IndexError, PreflightError:
        host = service = None
        reason = "external_cpu_source_unavailable_or_changed"
    return {
        "source": SOURCE,
        "phase": phase,
        "request_id": request,
        "read_started_ns": started,
        "read_finished_ns": clock(),
        "server_pid": pid,
        "process_start_ticks": ticks,
        "boot_id": boot,
        "clock_ticks_per_second": hz,
        "host_ticks": host,
        "service_ticks": service,
        "missing_reason": reason,
    }


def reduce_external_cpu(records, endpoint, interval_ms):
    intervals = []
    previous = None
    last_start = -1
    source = records[0].get("source") if records and isinstance(records[0], dict) else SOURCE
    macos = source == MACOS_SOURCE
    for row in records:
        if (
            not isinstance(row, dict)
            or source not in (SOURCE, MACOS_SOURCE)
            or row.get("source") != source
            or any(
                type(row.get(k)) is not int or row[k] < 0
                for k in ("read_started_ns", "read_finished_ns")
            )
            or row["read_finished_ns"] < row["read_started_ns"]
            or row["read_started_ns"] < last_start
        ):
            raise EvidenceError("invalid_external_cpu_record")
        last_start = row["read_started_ns"]
        if any(row.get(k) != endpoint[k] for k in ("server_pid", "process_start_ticks")):
            raise EvidenceError("external_cpu_process_binding_mismatch")
        host, service = row.get("host_ticks"), row.get("service_ticks")
        known = host is not None or service is not None
        if known and (
            not isinstance(host, list)
            or len(host) != (4 if macos else 8)
            or any(type(v) is not int or v < 0 for v in host)
            or type(service) is not int
            or service < 0
            or row.get("missing_reason") is not None
            or not isinstance(row.get("boot_id"), str)
            or not row["boot_id"]
            or type(row.get("clock_ticks_per_second")) is not int
            or row["clock_ticks_per_second"] <= 0
            or (macos and row["clock_ticks_per_second"] != 1_000_000)
        ):
            raise EvidenceError("invalid_external_cpu_counters")
        reason, value = "baseline_only", None
        at = (row["read_started_ns"] + row["read_finished_ns"]) // 2
        left = None
        if not known:
            reason = row.get("missing_reason") or "source_unavailable"
        elif previous is not None:
            left = (previous["read_started_ns"] + previous["read_finished_ns"]) // 2
            if previous.get("host_ticks") is None:
                reason = "previous_sample_missing"
            elif any(row[k] != previous[k] for k in ("boot_id", "clock_ticks_per_second")):
                reason = "counter_identity_changed"
            elif at <= left:
                reason = "nonpositive_interval"
            elif at - left > interval_ms * 2_000_000:
                reason = "sampling_gap"
            else:
                deltas = [a - b for a, b in zip(host, previous["host_ticks"], strict=True)]
                service_delta = service - previous["service_ticks"]
                total = sum(deltas)
                busy = total - deltas[3] - (0 if macos else deltas[4])
                if min(*deltas, service_delta) < 0:
                    reason = "counter_regression"
                elif total == 0:
                    reason = "zero_host_interval"
                elif service_delta > busy:
                    reason = "host_process_counter_skew"
                else:
                    value = (busy - service_delta) / total * 100
                    reason = None
        intervals.append(
            {
                "start_ns": left,
                "end_ns": at,
                "value_percent": value,
                "missing_reason": reason,
                "phase": row.get("phase"),
                "request_id": row.get("request_id"),
            }
        )
        previous = row
    values = [r["value_percent"] for r in intervals if r["value_percent"] is not None]
    return {
        "source": source,
        "intervals": intervals,
        "sample_count": len(records),
        "valid_intervals": len(values),
        "observed_max_percent": max(values, default=None),
        "comparison_eligible": False,
        "limitations": [
            "host_capacity_percent_not_single_core_percent",
            "host_busy_minus_bound_pid_includes_collector_kernel_and_other_work",
            "darwin_user_nice_system_idle_seconds_converted_to_microseconds"
            if macos
            else "iowait_excluded_guest_not_double_counted",
            "service_children_not_subtracted",
            "sequential_reads_not_atomic_no_negative_clamping",
            "request_coverage_and_frozen_threshold_assessed_separately",
        ],
    }
