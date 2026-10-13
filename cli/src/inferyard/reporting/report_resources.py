"""Aligned raw resource timelines; gaps and counter resets are never interpolated."""

from collections import defaultdict

SPECS = {
    "service_rss": ("服务 RSS", "MiB", 1 / 1024**2),
    "system_mem_available": ("系统可用内存", "MiB", 1 / 1024**2),
    "service_cpu_ticks": ("服务 CPU · 单核等效", "%", 1),
    "temperature": ("温度", "°C", 1),
    "frequency": ("频率", "GHz", 1 / 1e9),
}


def resource_view(samples, interval_ms):
    rows = [
        s
        for s in samples
        if s["metric_name"] in SPECS
        and (s["metric_name"] != "service_cpu_ticks" or s.get("capture") == "periodic")
    ]
    if not rows:
        return {"charts": [], "reason": "resource_samples_missing"}
    if len({s["clock_id"] for s in rows}) != 1:
        return {"charts": [], "reason": "resource_clock_mismatch"}
    origin = min(s["read_started_ns"] for s in rows)
    end = max(s["read_finished_ns"] for s in rows)
    span = max(1, end - origin)
    maximum_gap = interval_ms * 2 * 1_000_000
    groups = defaultdict(list)
    for row in rows:
        groups[(row["metric_name"], row["source"], row.get("semantics"))].append(row)
    charts = []
    priority = {name: i for i, name in enumerate(SPECS)}
    for (metric, source, semantics), series in sorted(
        groups.items(), key=lambda item: (priority[item[0][0]], item[0][1], item[0][2] or "")
    ):
        label, unit, factor = SPECS[metric]
        points, segments, current = [], [], []
        previous = None
        for sample in sorted(series, key=lambda s: (s["read_finished_ns"], s["seq"])):
            time = sample["read_finished_ns"]
            value, reason = sample["value"], sample.get("missing_reason")
            gap = previous is not None and time - previous["read_finished_ns"] > maximum_gap
            if metric == "service_cpu_ticks":
                reason = reason or ("baseline_required" if previous is None else None)
                hz = sample.get("clock_ticks_per_second")
                if previous is not None:
                    bound = all(
                        sample.get(k) == previous.get(k)
                        for k in (
                            "server_pid",
                            "process_start_ticks",
                            "boot_id",
                            "clock_ticks_per_second",
                        )
                    )
                    elapsed = time - previous["read_finished_ns"]
                    old = previous.get("value")
                    if (
                        not bound
                        or not hz
                        or elapsed <= 0
                        or old is None
                        or value is None
                        or value < old
                    ):
                        reason = reason or "cpu_delta_unavailable"
                    elif gap:
                        reason = "sampling_gap"
                    else:
                        value = (value - old) / hz / (elapsed / 1e9) * 100
            if reason is not None:
                value = None
            if value is not None:
                value *= factor
            point = {
                "seconds": (time - origin) / 1e9,
                "value": value,
                "reason": reason,
                "phase": sample["phase"],
                "sample_seq": sample["seq"],
                "gap_before": gap,
            }
            points.append(point)
            if value is None or gap:
                if current:
                    segments.append(current)
                current = []
            if value is not None:
                current.append(point)
            previous = sample
        if current:
            segments.append(current)
        values = [p["value"] for p in points if p["value"] is not None]
        low, high = min(values, default=0), max(values, default=0)
        extent = high - low or 1
        lines = []
        for segment in segments:
            coordinates = [
                [
                    round(p["seconds"] * 1e9 / span * 1000, 3),
                    round(110 - (p["value"] - low) / extent * 100, 3),
                ]
                for p in segment
            ]
            lines.append(
                {
                    "points": " ".join(f"{x},{y}" for x, y in coordinates),
                    "single": coordinates[0] if len(coordinates) == 1 else None,
                }
            )
        charts.append(
            {
                "metric": metric,
                "label": label,
                "source": source,
                "semantics": semantics,
                "unit": unit,
                "min": min(values, default=None),
                "max": max(values, default=None),
                "points": points,
                "lines": lines,
                "missing_count": sum(p["value"] is None for p in points),
                "gap_count": sum(p["gap_before"] for p in points),
                "reasons": sorted({p["reason"] for p in points if p["reason"]}),
            }
        )
    return {
        "charts": charts,
        "reason": None,
        "duration_seconds": (end - origin) / 1e9,
        "origin_ns": origin,
        "gap_threshold_seconds": maximum_gap / 1e9,
        "limitations": [
            "sampled_values_only",
            "independent_vertical_scales",
            "no_throttling_or_leak_inference",
            "cpu_single_core_equivalent_may_exceed_100_percent",
        ],
    }
