"""Pinned engine counters must not be replaced by client timing or visible text."""

import asyncio
import json

import httpx
import pytest

from inferyard.adapters.prism import PrismAdapter
from inferyard.contracts.engine_timing import capture_timings, engine_rates

RAW = {"cache_n": 20, "prompt_n": 80, "prompt_ms": 400, "predicted_n": 11, "predicted_ms": 500}


def row(raw=RAW, *, final=True, state="completed"):
    return {
        "execution_state": state,
        "engine_build_verified": True,
        "engine_timings": [capture_timings(raw, final=final)],
    }


def test_known_engine_timeline_uses_uncached_tokens_and_excludes_first_generated_token():
    result = engine_rates(row())
    assert result["L06"]["value"] == 200
    assert result["L07"]["value"] == 20
    assert result["L07"]["processed_tokens"] == 10
    assert result["L06"]["cache_tokens"] == 20


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
