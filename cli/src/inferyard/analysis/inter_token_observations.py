"""Inter-token and service-queue observations plus pooled block intervals."""

from collections import Counter

from inferyard.analysis.observations import Observations
from inferyard.analysis.performance import distribution, request_timing

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]
type Timing = list[tuple[JsonObject, JsonObject]]

N03_LIMITS = ("throughput_comparison_gate_pending",)
POOLED_STATISTICS = ("min", "p50", "p95", "max")


def _pooled_reason(
    reasons: list[str | None], completed_count: int, interval_count: int
) -> str | None:
    if interval_count:
        return None
    if not completed_count:
        return "no_completed_requests"
    captured = [reason for reason in reasons if reason != "evidence_missing"]
    if not captured:
        return "arrival_capture_missing"
    if all(reason == "not_streaming" for reason in captured):
        return "not_streaming"
    return "insufficient_blocks"


def _token_itl(output: Observations, timing: Timing) -> None:
    for row, _ in timing:
        output.add(
            "N03",
            "token_itl",
            None,
            request=row["request_id"],
            reason="no_token_level_event_source",
            comparison=False,
            limits=N03_LIMITS,
        )


def _service_queue(output: Observations) -> None:
    output.add(
        "N03",
        "service_queue_delay",
        None,
        reason="no_server_side_queue_events",
        comparison=False,
        limits=N03_LIMITS,
    )


def _missing_reasons(reasons: list[str | None]) -> Counter[str]:
    return Counter(
        "arrival_capture_missing" if item == "evidence_missing" else item
        for item in reasons
        if item is not None
    )


def _pooled_limits(
    reasons: list[str | None], intervals: list[float], requests: list[JsonObject]
) -> list[str]:
    limits = ["sample_unit_is_block_interval_not_request", "not_token_itl"]
    if any(value == 0 for value in intervals):
        limits.append("arrival_timestamps_batched")
    failed = sum(row["execution_state"] == "failed" for row in requests)
    if failed:
        limits.append(f"missing:failed:{failed}")
    limits.extend(
        f"missing:{name}:{count}" for name, count in sorted(_missing_reasons(reasons).items())
    )
    return limits


def _timing_reasons(timing: Timing) -> list[str | None]:
    return [metrics["L05"]["reason"] for _, metrics in timing]


def _timing_intervals(timing: Timing) -> list[float]:
    return [value for _, metrics in timing for value in metrics["L05"]["intervals"]]


def _pooled_intervals(
    output: Observations, requests: list[JsonObject], timing: Timing
) -> None:
    reasons = _timing_reasons(timing)
    intervals = _timing_intervals(timing)
    stats = distribution(intervals)
    reason = _pooled_reason(reasons, len(timing), len(intervals))
    limits = _pooled_limits(reasons, intervals, requests)
    for statistic in POOLED_STATISTICS:
        missing_reason = reason
        if missing_reason is None and statistic == "p95" and len(intervals) < 20:
            missing_reason = "insufficient_samples"
        extra = []
        if statistic == "p95" and stats["p95_exploratory"]:
            extra.append("exploratory_p95")
        output.add(
            "L05",
            "pooled_" + statistic,
            stats[statistic] if intervals else None,
            count=len(intervals),
            excluded=0,
            reason=missing_reason,
            source="decoded_delta_arrivals",
            limits=[*limits, *extra],
        )


def inter_token_observations(
    run: JsonObject,
    workload_id: str,
    requests: list[JsonObject],
    evidence: list[JsonObject],
    *,
    complete: bool,
) -> list[JsonObject]:
    """Return N03 placeholders and the pooled L05 block-interval distribution."""
    output = Observations(run, workload_id, evidence, complete=complete)
    completed = [row for row in requests if row["execution_state"] == "completed"]
    timing = [(row, request_timing(row)) for row in completed]
    _token_itl(output, timing)
    _service_queue(output)
    _pooled_intervals(output, requests, timing)
    return output.items
