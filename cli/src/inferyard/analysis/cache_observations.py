"""Observed prompt reuse counts, separate from requested policy and qualification."""

from inferyard.analysis.engine_timing import DEFINITION
from inferyard.runtime.cache_execution import request_value


def observe(records, state, build_verified, requested):
    final = [r for r in records if r.get("final") is True]
    reason = None
    if state != "completed":
        reason = "request_not_completed"
    elif not build_verified:
        reason = "engine_build_unverified"
    elif not final:
        reason = "final_engine_timings_missing"
    elif len(final) != 1:
        reason = "ambiguous_engine_timings"
    else:
        record = final[0]
        if record.get("definition") != DEFINITION:
            reason = "engine_timing_definition_unknown"
        elif record.get("missing_reason"):
            reason = record["missing_reason"]
        elif record.get("speculative") is not False:
            reason = "speculative_timing_scope_unverified"
        elif any(
            type(record.get(k)) is not int or not 0 <= record[k] <= 2**63 - 1
            for k in ("cache_n", "prompt_n")
        ):
            reason = "invalid_engine_counts"
    cached = final[0]["cache_n"] if reason is None else None
    processed = final[0]["prompt_n"] if reason is None else None
    return {
        "requested_reuse": requested,
        "cached_prompt_tokens": cached,
        "processed_prompt_tokens": processed,
        "reuse_observed": cached > 0 if cached is not None else None,
        "policy_consistent": (requested or cached == 0) if cached is not None else None,
        "missing_reason": reason,
        "source": DEFINITION,
    }


def cache_series(events, config, requests, *, build_verified):
    starts = [e for e in events if e["event_type"] == "request_started"]
    ends = {e["request_id"]: e["data"] for e in events if e["event_type"] == "request_finished"}
    timing = {}
    for event in events:
        if event["event_type"] == "engine_timings":
            timing.setdefault(event["request_id"], []).append(event["data"])
    rows = []
    for ordinal, event in enumerate(starts, 1):
        state = ends.get(event["request_id"], {}).get("execution_state", "unfinished")
        rows.append(
            {
                "ordinal": ordinal,
                "request_id": event["request_id"],
                "phase": event["phase"],
                "case_id": event["data"]["case_id"],
                "execution_state": state,
                **observe(
                    timing.get(event["request_id"], []),
                    state,
                    build_verified,
                    request_value(config, event["phase"], ordinal),
                ),
            }
        )
    for request in requests:
        if request["execution_state"] == "not_executed":
            rows.append(
                {
                    "ordinal": None,
                    "request_id": request["request_id"],
                    "phase": "formal",
                    "case_id": request["case_id"],
                    "execution_state": "not_executed",
                    **observe(
                        [],
                        "not_executed",
                        build_verified,
                        config["conditions"]["cache_policy"] == "enabled",
                    ),
                }
            )
    known = [r for r in rows if r["missing_reason"] is None]
    consistent = (
        False
        if any(r["policy_consistent"] is False for r in rows)
        else True
        if rows and len(known) == len(rows)
        else None
    )
    return {
        "definition": "observed_prefix_reuse_sequence.v1",
        "rows": rows,
        "planned_formal_requests": len(requests),
        "started_requests": len(starts),
        "observed_requests": len(known),
        "missing_requests": len(rows) - len(known),
        "policy_consistent": consistent,
        "initial_no_reuse_observed": (
            rows[0]["cached_prompt_tokens"] == 0
            if starts and rows[0]["missing_reason"] is None
            else None
        ),
        "comparison_eligible": False,
        "limitations": [
            "includes_probe_and_warmup_history_not_independent_cases",
            "no_os_cache_or_causal_speedup_claim",
            "counts_are_engine_reported_not_retokenized_text",
        ],
    }
