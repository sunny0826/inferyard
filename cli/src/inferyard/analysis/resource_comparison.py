"""Frozen, matched observed windows; never claim continuous or whole-host attribution."""

from inferyard.analysis.resource_metrics import sample_time, select_samples
from inferyard.evidence.storage import json_bytes

NAMES = {
    "C01": "system_mem_available",
    "C02": "service_rss",
    "C03": "service_rss",
    "C04": "service_cpu_ticks",
    "C05": "service_cpu_ticks",
    "C07": "temperature",
    "C08": "frequency",
}
SOURCES = {
    "system_mem_available": "/proc/meminfo:MemAvailable",
    "service_rss": "/proc/<pid>/status:VmRSS",
    "service_cpu_ticks": "/proc/<pid>/stat:utime+stime",
    "system_swap_in": "/proc/vmstat:pswpin",
    "system_swap_out": "/proc/vmstat:pswpout",
}
MACOS_SOURCES = {
    "system_mem_available": "psutil:virtual_memory:available",
    "service_rss": "psutil:Process.memory_info:rss",
    "service_cpu_ticks": "psutil:Process.cpu_times:user+system",
    "system_swap_in": "host_statistics64:swapins",
    "system_swap_out": "host_statistics64:swapouts",
}
MACOS_SWAP_SOURCES = {
    "system_swap_in": frozenset({"vm_stat:Swapins", "host_statistics64:swapins"}),
    "system_swap_out": frozenset({"vm_stat:Swapouts", "host_statistics64:swapouts"}),
}
STATISTICS = {
    "C01": {"observed_min"},
    "C02": {"observed_peak"},
    "C03": {"peak_minus_idle_baseline"},
    "C04": {"observed_cpu_seconds", "full_request_cpu_seconds"},
    "C05": {"observed_single_core_percent"},
    "C06": {
        name + suffix
        for name in ("system_swap_in", "system_swap_out")
        for suffix in ("_pages", "_bytes")
    },
    "C07": {"observed_max"},
    "C08": {"observed_min", "observed_max"},
}


def baseline(data, policy):
    collector = data.get("resource_collector", {})
    if not isinstance(collector, dict):
        return False
    sources = MACOS_SOURCES if collector.get("collector") == "macos-resource.v1" else SOURCES
    endpoint = data["config"]["endpoint"]
    starts = [r["t_send_ns"] for r in data["requests"] if r.get("t_send_ns") is not None]
    clocks = {r["clock_id"] for r in data["requests"] if r.get("clock_id")}
    samples = [
        s
        for s in data.get("samples", [])
        if s["metric_name"] == "service_rss"
        and s["phase"] == "baseline"
        and s["request_id"] is None
    ]
    valid = (
        bool(starts)
        and len(samples) >= policy["min_baseline_samples"]
        and all(
            s["value"] is not None
            and s["missing_reason"] is None
            and s["source"] == sources["service_rss"]
            and s["unit"] == "bytes"
            and s["clock_id"] in clocks
            and s["read_finished_ns"] <= min(starts)
            and all(s[k] == endpoint[k] for k in ("server_pid", "process_start_ticks"))
            for s in samples
        )
    )
    if not valid:
        return False
    times = sorted(sample_time(s) for s in samples)
    required = data["config"]["telemetry"]["baseline_seconds"] * 1e9 * policy["min_coverage_ratio"]
    gap = data["config"]["telemetry"]["interval_ms"] * 1e6 * policy["max_sample_gap_intervals"]
    return (
        times[-1] - times[0] >= required
        and max(b - a for a, b in zip(times, times[1:], strict=False)) <= gap
    )


def window(data, row, metric, policy):
    reasons = []
    code, statistic = metric["metric_id"], metric["statistic"]
    if statistic not in STATISTICS.get(code, set()):
        return {"reasons": ["resource_statistic_unsupported"]}
    name = NAMES.get(code)
    if code == "C06":
        name = next(
            (
                s
                for s in ("system_swap_in", "system_swap_out")
                if statistic in (s + "_pages", s + "_bytes")
            ),
            None,
        )
    if name is None:
        return {"reasons": ["resource_statistic_unsupported"]}
    collector = data.get("resource_collector", {})
    if not isinstance(collector, dict):
        return {"reasons": ["resource_collector_unsealed_or_unknown"]}
    collector_id = collector.get("collector")
    if collector_id not in ("linux-resource.v2", "macos-resource.v1"):
        reasons.append("resource_collector_unsealed_or_unknown")
    sources = MACOS_SOURCES if collector_id == "macos-resource.v1" else SOURCES
    interval_ms = data["config"]["telemetry"]["interval_ms"]
    if collector.get("interval_ms") != interval_ms:
        reasons.append("resource_collector_interval_mismatch")
    samples = data.get("samples", [])
    sensor = code in ("C07", "C08")
    if sensor:
        samples = [s for s in samples if s["source"] == metric["source"]]
    selected, excluded, missing = select_samples(row, samples, name, data["config"]["endpoint"])
    if missing:
        reasons.append("resource_samples_missing_or_source_changed")
    if metric.get("sample_count") != len(selected) or metric.get("excluded") != excluded:
        reasons.append("resource_sample_accounting_mismatch")
    if len(selected) < 2:
        return {"reasons": sorted(set(reasons + ["resource_window_insufficient_samples"]))}
    identities = {
        (
            s["source"],
            s["unit"],
            s.get("semantics"),
            s.get("clock_ticks_per_second"),
            s.get("page_size_bytes"),
        )
        for s in selected
    }
    if len(identities) != 1:
        reasons.append("resource_source_or_semantics_changed")
    elif not sensor:
        source = next(iter(identities))[0]
        allowed = MACOS_SWAP_SOURCES.get(name) if collector_id == "macos-resource.v1" else None
        if (source not in allowed) if allowed is not None else source != sources[name]:
            reasons.append("resource_source_unknown")
    identity = list(next(iter(identities))) if len(identities) == 1 else None
    if sensor:
        sensors = collector.get("sensors")
        sources = sensors.get("sources") if isinstance(sensors, dict) else None
        if not isinstance(sources, list) or any(not isinstance(s, dict) for s in sources):
            return {"reasons": sorted(set(reasons + ["resource_sensor_identity_unverified"]))}
        metadata = [
            s
            for s in sources
            if s.get("source") == metric["source"] and s.get("metric_name") == name
        ]
        if (
            len(metadata) != 1
            or not metadata[0].get("identity")
            or metadata[0].get("missing_reason")
            or any(metadata[0].get(k) is None for k in ("semantics", "unit", "scale"))
        ):
            reasons.append("resource_sensor_identity_unverified")
            identity = None
        else:
            meta = metadata[0]
            if any(
                s.get("semantics") != meta["semantics"] or s["unit"] != meta["unit"]
                for s in selected
            ):
                reasons.append("resource_sensor_semantics_mismatch")
            identity = [identity, meta["identity"], meta["scale"]]
    counter = code in ("C04", "C05", "C06")
    if counter:
        clocks = {
            (
                s.get("boot_id"),
                s.get("clock_ticks_per_second") if code != "C06" else s.get("page_size_bytes"),
            )
            for s in selected
        }
        if len(clocks) != 1 or None in next(iter(clocks)):
            reasons.append("resource_counter_identity_or_scale_unknown")
        scale_key = "page_size_bytes" if code == "C06" else "clock_ticks_per_second"
        if any(
            s.get("boot_id") != collector.get("boot_id")
            or s.get(scale_key) != collector.get(scale_key)
            or s.get("collector") != collector_id
            for s in selected
        ):
            reasons.append("resource_counter_collector_binding_mismatch")
        expected_scope = (
            (
                "host_swap_excludes_compression_not_model_attribution"
                if collector_id == "macos-resource.v1"
                else "host_including_swap_and_zram_not_model_attribution"
            )
            if code == "C06"
            else "bound_pid_all_threads_excludes_child_processes"
        )
        if (
            code != "C06"
            and collector_id == "macos-resource.v1"
            and (collector.get("clock_ticks_per_second") != 1_000_000)
        ):
            reasons.append("resource_counter_identity_or_scale_unknown")
        if collector.get("swap_scope" if code == "C06" else "cpu_scope") != expected_scope:
            reasons.append("resource_counter_scope_unknown")
    times = [sample_time(s) for s in selected]
    if any(
        b["read_started_ns"] < a["read_finished_ns"]
        for a, b in zip(selected, selected[1:], strict=False)
    ):
        reasons.append("resource_reads_overlap")
    max_gap = max(b - a for a, b in zip(times, times[1:], strict=False))
    if max_gap > interval_ms * 1e6 * policy["max_sample_gap_intervals"]:
        reasons.append("resource_sampling_gap_exceeds_frozen_limit")
    left = times[0] if counter else min(s["read_started_ns"] for s in selected)
    right = times[-1] if counter else max(s["read_finished_ns"] for s in selected)
    duration = row["t_terminal_ns"] - row["t_send_ns"]
    if duration <= 0:
        return {"reasons": sorted(set(reasons + ["resource_request_duration_invalid"]))}
    coverage = (right - left) / duration
    offsets = [(left - row["t_send_ns"]) / duration, (right - row["t_send_ns"]) / duration]
    if (metric.get("sampled_start_ns"), metric.get("sampled_end_ns")) != (
        left,
        right,
    ) or metric.get("coverage_ratio") != coverage:
        reasons.append("resource_window_recomputation_mismatch")
    if not 0 <= offsets[0] <= offsets[1] <= 1 or coverage < policy["min_coverage_ratio"]:
        reasons.append("resource_coverage_below_frozen_limit")
    if code == "C03" and not baseline(data, policy):
        reasons.append("resource_idle_baseline_incomplete")
    if statistic == "full_request_cpu_seconds" and coverage != 1:
        reasons.append("resource_full_request_boundaries_not_covered")
    return {
        "reasons": sorted(set(reasons)),
        "identity": identity,
        "offsets": offsets,
        "coverage": coverage,
        "samples": len(selected),
        "excluded": excluded,
        "max_gap_ns": max_gap,
    }


def compare_windows(left, right, metrics, cohorts, policies):
    reasons, windows = [], []
    if any(p is None for p in policies) or policies[0] != policies[1]:
        return ["resource_window_policy_missing_or_different"], []
    if any(len(c) != 1 for c in cohorts) or any(m is None for m in metrics):
        return ["resource_requires_one_request_per_case"], []
    for data, metric, cohort in zip((left, right), metrics, cohorts, strict=True):
        w = window(data, cohort[0], metric, policies[0])
        windows.append(w)
        reasons.extend(w["reasons"])
    if all("offsets" in w for w in windows):
        if any(
            abs(a - b) > policies[0]["max_window_offset_difference"]
            for a, b in zip(windows[0]["offsets"], windows[1]["offsets"], strict=True)
        ):
            reasons.append("resource_observed_windows_not_matched")
        if windows[0]["identity"] is None or json_bytes(windows[0]["identity"]) != json_bytes(
            windows[1]["identity"]
        ):
            reasons.append("resource_pair_source_identity_or_semantics_different")
    return sorted(set(reasons)), windows
