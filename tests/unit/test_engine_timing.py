"""Pinned engine counters must not be replaced by client timing or visible text."""

import asyncio
import json

import httpx
import pytest

from inferyard.adapters.prism import PrismAdapter
from inferyard.analysis.engine_timing import capture_timings, engine_rates

RAW = {"cache_n": 20, "prompt_n": 80, "prompt_ms": 400, "predicted_n": 11, "predicted_ms": 500}


def row(raw=RAW, *, final=True, state="completed"):
    return {
        "execution_state": state,
        "engine_build_verified": True,
        "engine_timings": [capture_timings(raw, final=final)],
    }


def test_known_engine_timeline_uses_uncached_tokens_and_excludes_first_generated_token():
    result = engine_rates(row())
    assert result["L06"]["value"] == 200  # 80 actually processed / 0.4 s; not 100 / 0.4 s
    assert result["L07"]["value"] == 20  # 10 decode steps / 0.5 s; not 11 / 0.5 s
    assert result["L07"]["processed_tokens"] == 10
    assert result["L06"]["cache_tokens"] == 20


@pytest.mark.parametrize(
    "changes,code,reason",
    [
        ({"prompt_n": 0}, "L06", "no_uncached_prompt_tokens"),
        ({"predicted_n": 1}, "L07", "no_decode_steps_after_first_token"),
        ({"predicted_n": 0}, "L07", "no_decode_steps_after_first_token"),
        ({"prompt_ms": 0}, "L06", "nonpositive_engine_duration"),
        ({"predicted_ms": 0}, "L07", "nonpositive_engine_duration"),
        ({"prompt_n": True}, "L06", "invalid_engine_timings"),
        ({"prompt_ms": -1}, "L06", "invalid_engine_timings"),
        ({"prompt_ms": float("inf")}, "L06", "invalid_engine_timings"),
        ({"predicted_n": 2**64}, "L07", "invalid_engine_timings"),
        ({"draft_n": 0}, "L07", "speculative_timing_scope_unverified"),
    ],
)
def test_engine_missingness(changes, code, reason):
    result = engine_rates(row({**RAW, **changes}))[code]
    assert result["value"] is None
    assert result["reason"] == reason


def test_no_final_no_evidence_and_failure_never_infer_decode_from_e2e():
    unknown = row()
    unknown["engine_build_verified"] = False
    assert engine_rates(unknown)["L07"]["reason"] == "engine_build_unverified"
    assert engine_rates(row(final=False))["L07"]["reason"] == "final_engine_timings_missing"
    assert (
        engine_rates(
            {
                "execution_state": "completed",
                "completion_tokens": 12,
                "t_first_content_ns": 200_000_000,
                "t_terminal_ns": 1_200_000_000,
            }
        )["L07"]["reason"]
        == "evidence_missing"
    )
    assert engine_rates(row(state="failed"))["L07"]["reason"] == "request_not_completed"
    duplicate = row()
    duplicate["engine_timings"] *= 2
    assert engine_rates(duplicate)["L07"]["reason"] == "ambiguous_engine_timings"


@pytest.mark.parametrize("streaming", [True, False])
@pytest.mark.parametrize(
    "raw,reason", [(RAW, None), ({"secret": "fixture-private-value"}, "invalid_engine_timings")]
)
def test_adapter_captures_only_numeric_metadata_and_optional_errors_do_not_fail_answer(
    streaming, raw, reason
):
    async def go():
        if streaming:
            payload = (
                "".join(
                    "data: " + json.dumps(value) + "\n\n"
                    for value in [
                        {
                            "choices": [
                                {"index": 0, "delta": {"content": "ok"}, "finish_reason": "stop"}
                            ]
                        },
                        {"choices": [], "usage": {"completion_tokens": 11}, "timings": raw},
                    ]
                )
                + "data: [DONE]\n\n"
            )
        else:
            payload = json.dumps(
                {
                    "choices": [{"message": {"content": "ok"}, "finish_reason": "stop"}],
                    "usage": {"completion_tokens": 11},
                    "timings": raw,
                }
            )
        events = []
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(
                lambda request: httpx.Response(200, content=payload.encode())
            ),
        )
        adapter.capture_arrivals = True
        try:
            terminal = await adapter.generate(
                {"stream": streaming}, 2, lambda *args: events.append(args)
            )
        finally:
            await adapter.close()
        return terminal, events

    terminal, events = asyncio.run(go())
    assert terminal["execution_state"] == "completed"
    timings = [data for kind, data, timestamp in events if kind == "engine_timings"]
    assert len(timings) == 1 and timings[0]["final"]
    assert timings[0]["missing_reason"] == reason
    assert "fixture-private-value" not in json.dumps(events)
    assert engine_rates({**terminal, "engine_build_verified": True, "engine_timings": timings})[
        "L07"
    ]["value"] == (20 if reason is None else None)
