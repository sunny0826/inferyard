"""S02 uses same-probe-cycle endpoints with explicit service idle evidence."""

from inferyard.analysis.observations import Observations

SOURCE = "/proc/<pid>/status:VmRSS:cycle_confirmed_idle"
MACOS_SOURCE = "psutil:Process.memory_info:rss:cycle_confirmed_idle"


def idle_rss_observations(
    run, workload, order, requests, samples, events, config, evidence, *, complete
):
    output = Observations(run, workload, evidence, complete=complete)
    points = []
    baseline = None
    baseline_source = None
    for index in range(len(order) - 1, len(requests), len(order)):
        row = requests[index]
        candidates = [
            s
            for s in samples
            if s["source"] in (SOURCE, MACOS_SOURCE) and s["request_id"] == row["request_id"]
        ]
        value = None
        reason = "idle_cycle_sample_missing"
        interval = None
        source = (
            candidates[0]["source"]
            if len(candidates) == 1
            else baseline_source or "cycle_confirmed_idle:source_unavailable"
        )
        if len(candidates) == 1:
            sample = candidates[0]
            idle = [
                e
                for e in events
                if e["event_type"] == "idle_observed"
                and e["request_id"] == row["request_id"]
                and e["phase"] == "residual"
                and e["data"]["state"] == "idle"
                and e["monotonic_ns"] <= sample["read_started_ns"]
            ]
            valid = (
                idle
                and sample["phase"] == "residual"
                and sample["metric_name"] == "service_rss"
                and sample["clock_id"] == row["clock_id"]
                and all(
                    sample[k] == config["endpoint"][k]
                    for k in ("server_pid", "process_start_ticks")
                )
            )
            if valid:
                value, reason = sample["value"], sample["missing_reason"]
                interval = (sample["read_started_ns"], sample["read_finished_ns"])
            else:
                reason = "idle_cycle_identity_or_idle_unverified"
        elif candidates:
            reason = "duplicate_idle_cycle_samples"
        cycle = index // len(order)
        if cycle == 0:
            baseline = value
        if baseline_source is None and len(candidates) == 1:
            baseline_source = source
        if baseline_source is not None and source != baseline_source:
            value, reason = None, "idle_cycle_source_changed"
        points.append(
            {
                "cycle_index": cycle,
                "request_id": row["request_id"],
                "rss_bytes": value,
                "missing_reason": reason,
            }
        )
        for stat, measurement, missing in (
            ("observed_idle_rss", value, reason),
            (
                "difference_from_first_cycle",
                value - baseline if value is not None and baseline is not None else None,
                reason or ("first_cycle_baseline_missing" if baseline is None else None),
            ),
        ):
            output.add(
                "S02",
                stat,
                measurement,
                request=row["request_id"],
                source=source,
                unit="bytes",
                count=int(value is not None),
                reason=missing,
                interval=interval,
                comparison=False,
                limits=[
                    "same_probe_cycle_confirmed_idle",
                    "not_evidence_of_memory_leak",
                    "cache_and_retained_pools_not_excluded",
                ],
            )
    if not points:
        output.add(
            "S02", "observed_idle_rss", None, reason="no_complete_probe_cycle", comparison=False
        )
    return {"points": points, "first_cycle_rss_bytes": baseline}, output.items
