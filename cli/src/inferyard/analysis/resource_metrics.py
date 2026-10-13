"""Resource windows never borrow warmup, residual or another process's samples."""

from collections import defaultdict
from statistics import median


def sample_time(sample):
    return (sample["read_started_ns"] + sample["read_finished_ns"]) // 2


def select_samples(row, samples, metric, endpoint):
    selected, excluded, reasons = [], 0, set()
    start, end = row.get("t_send_ns"), row.get("t_terminal_ns")
    for sample in samples:
        if sample["metric_name"] != metric or sample["request_id"] != row["request_id"]:
            continue
        if (
            start is None
            or end is None
            or sample["phase"] != "formal"
            or sample["clock_id"] != row["clock_id"]
            or sample["read_started_ns"] < start
            or sample["read_finished_ns"] > end
        ):
            excluded += 1
            continue
        if metric.startswith("service_") and any(
            sample[k] != endpoint[k] for k in ("server_pid", "process_start_ticks")
        ):
            reasons.add("source_changed")
            excluded += 1
            continue
        if sample["value"] is None:
            reasons.add(sample["missing_reason"])
            excluded += 1
        else:
            selected.append(sample)
    return sorted(selected, key=sample_time), excluded, sorted(reasons)


def counter_delta(row, samples, metric, endpoint):
    selected, excluded, reasons = select_samples(row, samples, metric, endpoint)
    result = {
        "value": None,
        "interval": None,
        "coverage": None,
        "sample_count": len(selected),
        "excluded": excluded,
        "reason": None,
        "limitations": ["counter_interval_uses_read_midpoints", "coverage_is_endpoint_span"],
        "scale": None,
        "largest_sample_gap_ns": None,
    }
    if row["execution_state"] not in ("completed", "failed"):
        result["reason"] = "request_not_validly_executed"
        return result
    if reasons:
        result["reason"] = reasons[0]
        result["limitations"].extend(reasons)
        return result
    if len(selected) < 2:
        result["reason"] = "insufficient_counter_samples"
        return result
    scale_key = "clock_ticks_per_second" if metric == "service_cpu_ticks" else "page_size_bytes"
    identities = {(s.get("boot_id"), s["source"], s.get(scale_key)) for s in selected}
    if len(identities) != 1 or None in next(iter(identities)):
        result["reason"] = "counter_source_or_scale_changed"
        return result
    pairs = list(zip(selected, selected[1:], strict=False))
    if any(b["value"] < a["value"] for a, b in pairs) or (
        metric == "service_cpu_ticks"
        and any(
            b["raw_cpu"][key] < a["raw_cpu"][key]
            for a, b in pairs
            for key in ("user_ticks", "system_ticks")
        )
    ):
        result["reason"] = "counter_reset"
        return result
    if any(b["read_started_ns"] < a["read_finished_ns"] for a, b in pairs):
        result["reason"] = "overlapping_counter_reads"
        return result
    first, last = selected[0], selected[-1]
    left, right = sample_time(first), sample_time(last)
    if right <= left or row["t_terminal_ns"] <= row["t_send_ns"]:
        result["reason"] = "nonpositive_counter_interval"
        return result
    result.update(
        value=last["value"] - first["value"],
        interval=(left, right),
        coverage=(right - left) / (row["t_terminal_ns"] - row["t_send_ns"]),
        scale=first[scale_key],
        largest_sample_gap_ns=max(sample_time(b) - sample_time(a) for a, b in pairs),
    )
    if result["coverage"] < 1:
        result["limitations"].append("partial_request_window")
    return result


def baseline_rss(requests, samples, endpoint):
    starts = [r["t_send_ns"] for r in requests if r.get("t_send_ns") is not None]
    first_start = min(starts) if starts else None
    clocks = {r["clock_id"] for r in requests if r.get("clock_id")}
    values = [
        s["value"]
        for s in samples
        if s["metric_name"] == "service_rss"
        and s["phase"] == "baseline"
        and s["request_id"] is None
        and s["clock_id"] in clocks
        and starts
        and s["read_finished_ns"] <= first_start
        and s["value"] is not None
        and all(s[k] == endpoint[k] for k in ("server_pid", "process_start_ticks"))
    ]
    return median(values) if values else None


def reduce_resources(requests, samples, config):
    endpoint = config["endpoint"]
    baseline = baseline_rss(requests, samples, endpoint)
    indexed = defaultdict(list)
    for sample in samples:
        indexed[sample["request_id"], sample["metric_name"]].append(sample)
    result = []
    for row in requests:
        if not row.get("request_id"):
            continue
        values = {}
        for metric, code, operation in (
            ("system_mem_available", "C01", min),
            ("service_rss", "C02", max),
        ):
            selected, excluded, reasons = select_samples(
                row, indexed[row["request_id"], metric], metric, endpoint
            )
            valid = row["execution_state"] in ("completed", "failed")
            values[code] = {
                "value": operation(s["value"] for s in selected)
                if selected and valid and "source_changed" not in reasons
                else None,
                "sample_count": len(selected),
                "excluded": excluded,
                "reason": "request_not_validly_executed"
                if not valid
                else "source_changed"
                if "source_changed" in reasons
                else "no_resource_samples",
                "limitations": ["sampled_extremum_not_instantaneous_peak", *reasons],
            }
            if values[code]["value"] is not None:
                values[code]["reason"] = None
                first = min(s["read_started_ns"] for s in selected)
                last = max(s["read_finished_ns"] for s in selected)
                values[code]["interval"] = (first, last)
                duration = row["t_terminal_ns"] - row["t_send_ns"]
                values[code]["coverage"] = (last - first) / duration if duration > 0 else None
                values[code]["limitations"].append(
                    "coverage_is_sample_span_not_continuous_observation"
                )
        values["C03"] = {
            **values["C02"],
            "value": values["C02"]["value"] - baseline
            if values["C02"]["value"] is not None and baseline is not None
            else None,
            "reason": values["C02"]["reason"] or ("baseline_missing" if baseline is None else None),
        }
        cpu = counter_delta(
            row, indexed[row["request_id"], "service_cpu_ticks"], "service_cpu_ticks", endpoint
        )
        if cpu["value"] is not None:
            cpu["value"] /= cpu["scale"]
        values["C04"] = cpu
        if (
            cpu["largest_sample_gap_ns"] is not None
            and cpu["largest_sample_gap_ns"] > config["telemetry"]["interval_ms"] * 2_000_000
        ):
            cpu["limitations"].append("sampling_gap_exceeds_two_intervals")
        values["C05"] = {
            **cpu,
            "value": cpu["value"] * 1e9 / (cpu["interval"][1] - cpu["interval"][0]) * 100
            if cpu["value"] is not None
            else None,
        }
        values["C06"] = {
            name: counter_delta(row, indexed[row["request_id"], name], name, endpoint)
            for name in ("system_swap_in", "system_swap_out")
        }
        result.append(
            {"request_id": row["request_id"], "category": row["category"], "metrics": values}
        )
    return {"baseline_rss_bytes": baseline, "requests": result}
