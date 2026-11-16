"""Batch-window throughput observations; one reduced series per metric group."""

from collections import Counter
from dataclasses import dataclass

from inferyard.analysis.observations import Observations
from inferyard.analysis.performance import request_timing

type JsonScalar = str | int | float | bool | None
type JsonNode = JsonScalar | list[JsonNode] | dict[str, JsonNode]
type JsonObject = dict[str, JsonNode]
type Window = tuple[float | None, str | None]

THROUGHPUT_LIMITS = ("throughput_comparison_gate_pending", "serial_trial_no_concurrency")


@dataclass(frozen=True)
class ThroughputInput:
    """Immutable trial context for the three batch-window rates."""

    run: JsonObject
    workload_id: str
    evidence: list[JsonObject]
    complete: bool
    thresholds: JsonObject | None


def _window(requests: list[JsonObject]) -> Window:
    sends = [row["t_send_ns"] for row in requests if type(row.get("t_send_ns")) is int]
    terminals = [row["t_terminal_ns"] for row in requests if type(row.get("t_terminal_ns")) is int]
    if not sends:
        return None, "no_formal_requests"
    if not terminals or max(terminals) <= min(sends):
        return None, "terminal_window_unavailable"
    return (max(terminals) - min(sends)) / 1e9, None


def _completed(requests: list[JsonObject]) -> list[JsonObject]:
    return [row for row in requests if row["execution_state"] == "completed"]


def _sent(requests: list[JsonObject]) -> int:
    return sum(type(row.get("t_send_ns")) is int for row in requests)


def _completion_tokens(request: JsonObject) -> int | None:
    count = request.get("completion_tokens")
    usage = (
        request.get("token_source") == "endpoint.usage"
        and request.get("token_scope") == "completion_tokens"
    )
    return count if usage and type(count) is int and count >= 0 else None


def _quality_is_applicable(requests: list[JsonObject]) -> bool:
    return not any(row.get("quality_state") == "not_applicable" for row in requests)


def _goodput_counts(
    completed: list[JsonObject], maximum_ms: float
) -> tuple[int, Counter[str]]:
    accepted, indeterminate = 0, Counter()
    for row in completed:
        state, latency = row.get("quality_state"), request_timing(row)["L03"]["value"]
        if state == "fail" or (latency is not None and latency > maximum_ms):
            continue
        if state == "pass" and latency is not None:
            accepted += 1
            continue
        indeterminate["latency_missing" if state == "pass" else "quality_unscorable"] += 1
    return accepted, indeterminate


def _request_rate(
    output: Observations, requests: list[JsonObject], window: Window
) -> None:
    seconds, reason = window
    output.add(
        "N01",
        "requests_per_second",
        len(_completed(requests)) / seconds if seconds else None,
        count=_sent(requests),
        reason=reason,
        comparison=False,
        limits=THROUGHPUT_LIMITS,
    )


def _output_token_rate(
    output: Observations, requests: list[JsonObject], window: Window
) -> None:
    seconds, reason = window
    completed = _completed(requests)
    counts = [(row, _completion_tokens(row)) for row in completed]
    missing = [row for row, count in counts if count is None]
    total = sum(count for _, count in counts if count is not None)
    if reason is not None:
        reason, value = reason, None
    elif missing:
        reason, value = "output_token_counts_incomplete", None
    elif not completed:
        reason, value = "no_completed_requests", None
    else:
        reason, value = None, total / seconds
    output.add(
        "N04",
        "output_tokens_per_second",
        value,
        count=len(counts) - len(missing),
        excluded=len(missing),
        reason=reason,
        source="endpoint.usage/completion_tokens_over_batch_window",
        comparison=False,
        limits=[
            *THROUGHPUT_LIMITS,
            *(["output_token_counts_incomplete"] if missing else []),
        ],
    )


def _goodput_rate(
    output: Observations,
    requests: list[JsonObject],
    window: Window,
    thresholds: JsonObject | None,
) -> None:
    seconds, reason = window
    accepted, indeterminate = (
        _goodput_counts(_completed(requests), thresholds["e2e_latency_ms_max"])
        if thresholds is not None
        else (0, Counter())
    )
    if reason is not None:
        reason, value = reason, None
    elif not _quality_is_applicable(requests):
        reason, value = "quality_not_applicable", None
    elif thresholds is None:
        reason, value = "thresholds_not_preregistered", None
    elif indeterminate:
        reason, value = "goodput_indeterminable", None
    else:
        reason, value = None, accepted / seconds
    output.add(
        "N02",
        "goodput_per_second",
        value,
        count=_sent(requests),
        reason=reason,
        comparison=False,
        limits=[
            *THROUGHPUT_LIMITS,
            *[f"missing:{name}:{count}" for name, count in sorted(indeterminate.items())],
        ],
    )


def throughput_observations(
    inputs: ThroughputInput, requests: list[JsonObject]
) -> list[JsonObject]:
    """Return N01/N02/N04 observations over the frozen trial window."""
    output = Observations(
        inputs.run,
        inputs.workload_id,
        inputs.evidence,
        complete=inputs.complete,
    )
    window = _window(requests)
    _request_rate(output, requests, window)
    _output_token_rate(output, requests, window)
    _goodput_rate(output, requests, window, inputs.thresholds)
    return output.items
