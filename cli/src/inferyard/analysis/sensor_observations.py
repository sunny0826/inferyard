"""Per-source observed extrema; retain the raw timeline in memory.jsonl."""

from inferyard.analysis.resource_metrics import select_samples


def add_sensor_observations(output, requests, samples):
    requests = [row for row in requests if row.get("request_id")]
    if not requests:
        return
    grouped = {"temperature": {}, "frequency": {}}
    for sample in samples:
        metric = sample["metric_name"]
        if metric in grouped:
            identity = (sample["source"], sample["semantics"])
            by_request = grouped[metric].setdefault(identity, {})
            by_request.setdefault(sample["request_id"], []).append(sample)
    # Discover sources across the whole timeline, even when a request has no
    # samples for one source. Keep input order inside each bucket for selection.
    sources_by_metric = {metric: sorted(sources) for metric, sources in grouped.items()}
    for row in requests:
        for metric, code, unit in (("temperature", "C07", "celsius"), ("frequency", "C08", "Hz")):
            sources = sources_by_metric[metric]
            if not sources:
                output.add(
                    code,
                    "observed_max",
                    None,
                    request=row["request_id"],
                    category=row["category"],
                    reason="sensor_evidence_unavailable",
                    unit=unit,
                )
            for source, semantics in sources:
                subset = grouped[metric][source, semantics].get(row["request_id"], ())
                selected, excluded, reasons = select_samples(row, subset, metric, {})
                valid = row["execution_state"] in ("completed", "failed")
                usable = selected and valid and "source_changed" not in reasons
                interval = None
                coverage = None
                if usable:
                    interval = (
                        min(s["read_started_ns"] for s in selected),
                        max(s["read_finished_ns"] for s in selected),
                    )
                    duration = row["t_terminal_ns"] - row["t_send_ns"]
                    coverage = (interval[1] - interval[0]) / duration if duration > 0 else None
                for statistic, operation in [
                    ("observed_max", max),
                    *([("observed_min", min)] if metric == "frequency" else []),
                ]:
                    output.add(
                        code,
                        statistic,
                        operation(s["value"] for s in selected) if usable else None,
                        request=row["request_id"],
                        category=row["category"],
                        source=source,
                        unit=unit,
                        count=len(selected),
                        excluded=excluded,
                        reason="request_not_validly_executed"
                        if not valid
                        else "source_changed"
                        if "source_changed" in reasons
                        else reasons[0]
                        if reasons
                        else "no_sensor_samples_in_window",
                        interval=interval,
                        coverage=coverage,
                        limits=[
                            semantics,
                            "no_throttling_inference",
                            "sampled_extremum_not_instantaneous_peak",
                            "coverage_is_sample_span_not_continuous_observation",
                            *reasons,
                        ],
                    )
