"""Prism adfffbe41 timings mapping, not portable across engine builds.

server-common.h: n_gen_steps() excludes the first sampled token; t_gen_ms()
starts after that sample. prompt_n excludes cached tokens. Scope includes model
sampling (reasoning and special/stop tokens), not just visible answer text.
"""

import math

DEFINITION = "prism.adfffbe41.timings.v1"
COUNTS = ("cache_n", "prompt_n", "predicted_n")
DURATIONS = ("prompt_ms", "predicted_ms")


def capture_timings(raw, *, final):
    values = {key: None for key in (*COUNTS, *DURATIONS)}
    valid = type(raw) is dict
    if valid:
        valid = all(type(raw.get(k)) is int and 0 <= raw[k] <= 2**63 - 1 for k in COUNTS)
        valid = valid and all(
            type(raw.get(k)) in (int, float) and 0 <= raw[k] <= 1e15 and math.isfinite(raw[k])
            for k in DURATIONS
        )
    if valid:
        values = {key: raw[key] for key in values}
    return {
        "definition": DEFINITION,
        "final": bool(final),
        "speculative": type(raw) is dict and any(k in raw for k in ("draft_n", "draft_n_accepted")),
        "missing_reason": None if valid else "invalid_engine_timings",
        **values,
    }


def engine_rates(row):
    records = row.get("engine_timings", [])
    final = [r for r in records if r["final"]]
    reason = None
    if row.get("execution_state") != "completed":
        reason = "request_not_completed"
    elif not records:
        reason = "evidence_missing"
    elif not row.get("engine_build_verified", False):
        reason = "engine_build_unverified"
    elif len(final) != 1:
        reason = "final_engine_timings_missing" if not final else "ambiguous_engine_timings"
    else:
        record = final[0]
        if record["definition"] != DEFINITION:
            reason = "engine_timing_definition_unknown"
        elif record["missing_reason"]:
            reason = record["missing_reason"]
        elif record["speculative"]:
            reason = "speculative_timing_scope_unverified"
    result = {}
    for code, count_key, duration_key in (
        ("L06", "prompt_n", "prompt_ms"),
        ("L07", "predicted_n", "predicted_ms"),
    ):
        missing, count, seconds = reason, None, None
        if missing is None:
            record = final[0]
            count = record[count_key] if code == "L06" else max(0, record[count_key] - 1)
            seconds = record[duration_key] / 1000
            if count == 0:
                missing = (
                    "no_uncached_prompt_tokens"
                    if code == "L06"
                    else "no_decode_steps_after_first_token"
                )
            elif seconds <= 0:
                missing = "nonpositive_engine_duration"
        result[code] = {
            "value": count / seconds if missing is None else None,
            "unit": "token/s",
            "reason": missing,
            "source": DEFINITION,
            "processed_tokens": count,
            "duration_seconds": seconds,
            "cache_tokens": final[0]["cache_n"] if len(final) == 1 else None,
        }
    return result
