"""Sampled memory cards: per-source extrema in MiB; nothing is pooled or interpolated."""

MIB = 1024**2
MEMORY_METRICS = {"service_rss": max, "system_mem_available": min}


def memory_view(samples):
    """Reduce memory samples without chart coordinates or cross-source pooling."""
    rows = [sample for sample in samples if sample["metric_name"] in MEMORY_METRICS]
    result = {metric: {} for metric in MEMORY_METRICS}
    if not rows:
        return {**result, "reason": "resource_samples_missing"}
    if len({row["clock_id"] for row in rows}) != 1:
        return {**result, "reason": "resource_clock_mismatch"}
    for metric, aggregate in MEMORY_METRICS.items():
        selected = [row for row in rows if row["metric_name"] == metric]
        for source in sorted({row["source"] for row in selected}):
            values = [
                row["value"] / MIB
                for row in selected
                if row["source"] == source and row["value"] is not None
            ]
            result[metric][source] = aggregate(values) if values else None
    return {**result, "reason": None}
