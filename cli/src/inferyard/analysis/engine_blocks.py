"""Interleaved block verdict for the same-GGUF engine comparison (ADR 056).

Only client-side timings decide. A winner requires every gate to pass and
every block pair's median difference to point the same way.
"""

import statistics

from inferyard.analysis.engine_comparison import PENDING
from inferyard.contracts.engine_profiles import TUNING_KEYS
from inferyard.contracts.engine_tuning import observe

DEFINITION = "engine-comparison.v1"
MIN_BLOCKS = 3
# metric -> True when a larger value is better
METRICS = {
    "first_content_ms": False,
    "e2e_ms": False,
    "output_tokens_per_second": True,
    "client_decode_interval_ms": False,
}
METRIC_IDS = {
    "first_content_ms": "L01",
    "e2e_ms": "L03",
    "output_tokens_per_second": "L04",
    "client_decode_interval_ms": "client_decode_interval",
}
SIDE_SCOPE = ("model", "engine", "generation", "conditions", "execution", "safety")


def _service(data):
    service = (
        data.get("identity", {}).get("local_engine", {}).get("binding", {}).get("service") or {}
    )
    if type(service.get("pid")) is not int or service.get("start_ticks") is None:
        return None
    return service["pid"], service["start_ticks"]


def _side_scope(data):
    return (
        {key: data["config"].get(key) for key in SIDE_SCOPE},
        data["selection"]["bundle_sha256"],
        data["selection"]["case_ids"],
    )


def _order(blocks, blockers):
    if any(not isinstance(d.get("first_event_utc"), str) for d in blocks):
        blockers.append("block_start_time_unknown")
        return list(blocks)
    return sorted(blocks, key=lambda d: d["first_event_utc"])


def _structure(ordered, blockers):
    families = [d["config"]["engine"].get("family") for d in ordered]
    if any(a == b for a, b in zip(families, families[1:], strict=False)):
        blockers.append("blocks_not_interleaved")
    left = [d for d in ordered if d["config"]["engine"].get("family") == families[0]]
    right = [d for d in ordered if d["config"]["engine"].get("family") != families[0]]
    if len(left) != len(right) or len(left) < MIN_BLOCKS:
        blockers.append("insufficient_blocks")
    for side in (left, right):
        if any(_side_scope(d) != _side_scope(side[0]) for d in side[1:]):
            blockers.append("side_configuration_changed")
    services = [_service(d) for d in ordered]
    if any(s is None for s in services):
        blockers.append("block_service_unknown")
    elif len(set(services)) != len(services):
        blockers.append("block_service_not_replaced")
    return left, right


def _usage(row, field):
    detail = row.get("usage_detail") or {}
    value = detail.get(field)
    return value if type(value) is int else None


def _metrics(row):
    tokens = row["completion_tokens"]
    e2e = (row["t_terminal_ns"] - row["t_send_ns"]) / 1e6
    first = row.get("t_first_content_ns")
    ttft = (first - row["t_send_ns"]) / 1e6 if first is not None else None
    return {
        "first_content_ms": ttft,
        "e2e_ms": e2e,
        "output_tokens_per_second": tokens / (e2e / 1e3) if e2e > 0 else None,
        "client_decode_interval_ms": (e2e - ttft) / (tokens - 1)
        if ttft is not None and tokens > 1
        else None,
    }


def _pair_requests(left, right, blockers, mismatches):
    rows = {r["plan_index"]: r for r in right["requests"]}
    pairs, excluded = [], {}
    for a in left["requests"]:
        b = rows.get(a["plan_index"])
        if b is None or b["case_id"] != a["case_id"]:
            blockers.append("request_order_differs")
            continue
        if a["execution_state"] != "completed" or b["execution_state"] != "completed":
            excluded["not_completed"] = excluded.get("not_completed", 0) + 1
            continue
        prompts = (_usage(a, "prompt_tokens"), _usage(b, "prompt_tokens"))
        if None in prompts:
            blockers.append("prompt_tokens_unobserved")
        elif prompts[0] != prompts[1]:
            blockers.append("prompt_tokens_differ")
            mismatches.append({"case_id": a["case_id"], "prompt_tokens": list(prompts)})
        for side, row in (("left", a), ("right", b)):
            cached = _usage(row, "cached_tokens")
            if cached is None:
                blockers.append(side + ".cold_cache_unverified")
            elif cached > 0:
                blockers.append(side + ".cache_hit_observed")
        counts = (a["completion_tokens"], b["completion_tokens"])
        if None in counts or counts[0] != counts[1]:
            excluded["output_tokens_differ"] = excluded.get("output_tokens_differ", 0) + 1
            continue
        pairs.append((_metrics(a), _metrics(b)))
    return pairs, excluded


def _median(values):
    values = [v for v in values if v is not None]
    return statistics.median(values) if values else None


def _tuning(left, right):
    a, b = observe(left["config"]), observe(right["config"])
    rows = []
    for key in TUNING_KEYS:
        known = a[key]["value"] is not None and b[key]["value"] is not None
        status = "unknown" if not known else "same" if a[key] == b[key] else "different"
        rows.append({"key": key, "left": a[key], "right": b[key], "status": status})
    scope = (
        "engine_implementation"
        if all(row["status"] == "same" for row in rows)
        else "product_configuration"
    )
    return rows, scope


def _verdict(metric, deltas, eligible, families):
    if not eligible:
        return "not_eligible"
    if not deltas or any(d is None or d == 0 for d in deltas):
        return "no_clear_difference"
    signs = {d > 0 for d in deltas}
    if len(signs) != 1:
        return "no_clear_difference"
    right_better = signs.pop() == METRICS[metric]
    return families[1] if right_better else families[0]


def compare_blocks(blocks):
    """Blocks are verified trial data carrying first_event_utc, in any order."""
    from inferyard.analysis.comparison import compare_trials

    blockers, mismatches = [], []
    ordered = _order(blocks, blockers)
    left, right = _structure(ordered, blockers)
    if not left or not right:
        from inferyard.evidence.storage import EvidenceError

        raise EvidenceError("engine_comparison_requires_two_families")
    conditions = None
    block_rows = []
    for index, (a, b) in enumerate(zip(left, right, strict=False)):
        identity = compare_trials(a, b, mode="engine")
        conditions = conditions if conditions is not None else identity["conditions"]
        blockers.extend(x for x in identity["blockers"] if x not in PENDING)
        pairs, excluded = _pair_requests(a, b, blockers, mismatches)
        medians = {
            metric: [_median(p[side][metric] for p in pairs) for side in (0, 1)]
            for metric in METRICS
        }
        block_rows.append(
            {
                "block": index + 1,
                "run_ids": [a["run"]["run_id"], b["run"]["run_id"]],
                "paired_requests": len(pairs),
                "excluded": excluded,
                "medians": {
                    metric: {
                        "left": pair[0],
                        "right": pair[1],
                        "difference": pair[1] - pair[0] if None not in pair else None,
                    }
                    for metric, pair in medians.items()
                },
            }
        )
        if not pairs:
            blockers.append("block_without_paired_requests")
    blockers = list(dict.fromkeys(blockers))
    families = [left[0]["config"]["engine"]["family"], right[0]["config"]["engine"]["family"]]
    tuning, scope = _tuning(left[0], right[0])
    eligible = not blockers
    verdicts = {
        metric: {
            "metric_id": METRIC_IDS[metric],
            "higher_is_better": higher,
            "verdict": _verdict(
                metric,
                [row["medians"][metric]["difference"] for row in block_rows],
                eligible,
                families,
            ),
        }
        for metric, higher in METRICS.items()
    }
    return {
        "definition": DEFINITION,
        "families": families,
        "difference_direction": "right_minus_left",
        "block_order": [d["run"]["run_id"] for d in ordered],
        "conditions": conditions or [],
        "tuning": tuning,
        "scope": scope,
        "blocks": block_rows,
        "prompt_token_mismatches": mismatches,
        "verdicts": verdicts,
        "eligible": eligible,
        "blockers": blockers,
        "limitations": [
            "client_timings_include_measurement_overhead",
            "effective_parameters_not_verified_native",
            "engine_reported_timings_not_used_for_verdict",
            *(["engine_defaults_not_controlled"] if scope == "product_configuration" else []),
        ],
    }
