"""Read-only Linux thermal timeline; never starts or stops a workload."""

import argparse
import datetime
import hashlib
import json
import math
import os
import signal
import time
from pathlib import Path


def read(path):
    try:
        return path.read_text().strip()
    except OSError as exc:
        return {"unavailable": type(exc).__name__}


def number(value):
    try:
        result = float(value)
        return result if math.isfinite(result) else None
    except TypeError, ValueError:
        return None


def discover(root):
    channels = {}
    definitions = (
        ("class/thermal/thermal_zone*/temp", "temperature", "type", 1000),
        ("class/hwmon/hwmon*/temp*_input", "temperature", "name", 1000),
        ("class/hwmon/hwmon*/fan*_input", "fan_rpm", "name", 1),
        ("devices/system/cpu/cpu*/cpufreq/scaling_cur_freq", "reported_mhz", None, 1000),
        ("devices/system/cpu/cpu*/thermal_throttle/*_count", "throttle_count", None, 1),
        ("class/powercap/intel-rapl:*/energy_uj", "energy_uj", "name", 1),
    )
    for pattern, kind, label, scale in definitions:
        for path in sorted(root.glob(pattern)):
            metadata = {"kind": kind, "scale": scale, "path": str(path)}
            if label:
                metadata["label"] = read(path.parent / label)
            if kind == "temperature" and path.name.endswith("_input"):
                metadata["sensor_label"] = read(path.with_name(path.name.replace("input", "label")))
            if kind == "energy_uj":
                metadata["max_energy_range_uj"] = number(read(path.parent / "max_energy_range_uj"))
            channels[str(path)] = metadata
    return channels


def process_stat(proc, pid):
    raw = read(proc / str(pid) / "stat")
    if not isinstance(raw, str):
        return raw
    try:
        fields = raw[raw.rindex(")") + 2 :].split()
        return {"ticks": int(fields[11]) + int(fields[12]), "start_ticks": int(fields[19])}
    except ValueError, IndexError:
        return {"unavailable": "invalid_proc_stat"}


def host_stat(proc):
    raw = read(proc / "stat")
    if not isinstance(raw, str):
        return raw
    try:
        # guest and guest_nice are already included in user and nice.
        fields = [int(x) for x in raw.splitlines()[0].split()[1:9]]
        return {"total": sum(fields), "idle": fields[3] + fields[4]}
    except ValueError, IndexError:
        return {"unavailable": "invalid_proc_stat"}


def snapshot(channels, proc, pids):
    started = time.monotonic()
    return {
        "monotonic": started,
        "utc": datetime.datetime.now(datetime.UTC).isoformat(),
        "boot_id": read(proc / "sys/kernel/random/boot_id"),
        "values": {p: read(Path(p)) for p in channels},
        "host_cpu": host_stat(proc),
        "processes": {str(pid): process_stat(proc, pid) for pid in pids},
        "read_span_seconds": time.monotonic() - started,
    }


def derive(previous, current, channels, ticks_per_second):
    result = {"host_cpu_percent": None, "process_cpu_percent_one_core": {}, "channels": {}}
    if previous is None:
        return result
    elapsed = current["monotonic"] - previous["monotonic"]
    if elapsed <= 0 or not isinstance(current["boot_id"], str):
        return result
    if current["boot_id"] != previous["boot_id"]:
        return result
    a, b = previous["host_cpu"], current["host_cpu"]
    if all(k in a and k in b for k in ("total", "idle")):
        total, idle = b["total"] - a["total"], b["idle"] - a["idle"]
        if total > 0 and 0 <= idle <= total:
            result["host_cpu_percent"] = 100 * (total - idle) / total
    for pid, b in current["processes"].items():
        a = previous["processes"].get(pid, {})
        value = None
        if (
            ticks_per_second is not None
            and ticks_per_second > 0
            and "ticks" in a
            and "ticks" in b
            and a["start_ticks"] == b["start_ticks"]
        ):
            delta = b["ticks"] - a["ticks"]
            if delta >= 0:
                value = 100 * delta / ticks_per_second / elapsed
        result["process_cpu_percent_one_core"][pid] = value
    for path, metadata in channels.items():
        a = number(previous["values"].get(path))
        b = number(current["values"].get(path))
        value, reason = None, None
        kind = metadata["kind"]
        if kind not in ("throttle_count", "energy_uj"):
            continue
        if a is None or b is None:
            reason = "missing_counter"
        elif b < a:
            # Counter wrap and reset cannot be distinguished from two observations.
            reason = "counter_decreased_wrap_or_reset"
        elif kind == "energy_uj":
            value = (b - a) / 1_000_000 / elapsed
        else:
            value = b - a
        result["channels"][path] = {
            "value": value,
            "unit": "watts_interval_average" if kind == "energy_uj" else "events",
            "reason": reason,
        }
    return result


def collect(out, *, seconds, interval, pids=(), phase="observation", sys_root=None, proc=None):
    if not all(math.isfinite(v) and v > 0 for v in (seconds, interval)):
        raise ValueError("duration and interval must be finite and positive")
    if seconds > 3600 or interval < 0.1 or interval > seconds:
        raise ValueError("duration <= 3600; 0.1 <= interval <= duration required")
    if any(type(pid) is not int or pid <= 0 for pid in pids):
        raise ValueError("PIDs must be positive integers")
    if os.name == "nt" and sys_root is None and proc is None:
        raise RuntimeError("linux_thermal_diagnostic_required")
    out.mkdir(parents=True, exist_ok=False)
    channels = discover(sys_root or Path("/sys"))
    proc = proc or Path("/proc")
    ticks = os.sysconf("SC_CLK_TCK") if hasattr(os, "sysconf") else None
    identities = {str(pid): process_stat(proc, pid) for pid in pids}
    metadata = {
        "definition": "thermal_diagnostic.v1",
        "channels": channels,
        "phase": phase,
        "pids": list(pids),
        "process_identities": identities,
        "process_names": {str(pid): read(proc / str(pid) / "comm") for pid in pids},
        "ticks_per_second": ticks,
        "requested_seconds": seconds,
        "requested_interval_seconds": interval,
        "script_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "limitations": [
            "read_only_observer_not_a_workload_safety_supervisor",
            "sequential_reads_not_simultaneous_and_can_miss_peaks",
            "reported_frequency_not_actual_busy_frequency",
            "host_energy_not_attributable_to_one_process",
            "process_cpu_excludes_children_and_uses_one_core_denominator",
            "counter_decreases_unknown_not_assumed_wrap",
            "unobserved_multiple_energy_wraps_not_detectable",
            "no_cross_boot_or_reused_pid_counter_deltas",
        ],
    }
    (out / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
    previous, count, gaps, unavailable = None, 0, [], set()
    start = time.monotonic()
    terminal = {"state": "interrupted", "samples": 0}
    try:
        with (out / "samples.jsonl").open("w") as stream:
            while True:
                row = snapshot(channels, proc, pids)
                for pid, observed in row["processes"].items():
                    expected = identities[pid].get("start_ticks")
                    if expected is None or observed.get("start_ticks") != expected:
                        row["processes"][pid] = {"unavailable": "initial_process_identity_lost"}
                row["phase"] = phase
                row["derived"] = derive(previous, row, channels, ticks)
                if previous:
                    gaps.append(row["monotonic"] - previous["monotonic"])
                unavailable.update(p for p, v in row["values"].items() if number(v) is None)
                stream.write(json.dumps(row) + "\n")
                stream.flush()
                count += 1
                previous = row
                remaining = seconds - (time.monotonic() - start)
                if remaining <= 0:
                    break
                time.sleep(min(interval, remaining))
        terminal["state"] = "completed"
    finally:
        terminal.update(
            samples=count,
            elapsed_seconds=time.monotonic() - start,
            maximum_sample_gap_seconds=max(gaps, default=None),
            unavailable_channels=sorted(unavailable),
            observed_processes=list(pids),
            workload_started_or_stopped=False,
        )
        (out / "terminal.json").write_text(json.dumps(terminal, indent=2) + "\n")
    return terminal


def interrupted(signum, frame):
    raise KeyboardInterrupt(f"diagnostic interrupted by signal {signum}")


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, interrupted)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--seconds", type=float, default=60)
    parser.add_argument("--interval", type=float, default=1)
    parser.add_argument("--pid", type=int, action="append", default=[])
    parser.add_argument("--phase", default="observation")
    args = parser.parse_args()
    print(
        json.dumps(
            collect(
                args.out,
                seconds=args.seconds,
                interval=args.interval,
                pids=args.pid,
                phase=args.phase,
            )
        )
    )
