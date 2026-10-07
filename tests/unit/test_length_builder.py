import asyncio

import pytest

from inferyard.evidence.storage import EvidenceError
from inferyard.runtime.length_builder import fit_length


def spec(**updates):
    return {
        "prefix": "开始",
        "suffix": "结束",
        "padding_unit": "填",
        "target_tokens": 23,
        "tolerance_tokens": 0,
        "max_repetitions": 100,
        "max_probes": 32,
        "max_characters": 100,
        "max_wall_seconds": 10,
        **updates,
    }


def test_constructs_measured_text_with_prefix_suffix_and_bounded_probes():
    async def counter(prompt):
        return {"input_tokens": len(prompt), "verification": "verified"}

    result = asyncio.run(fit_length(spec(), counter))
    assert result["status"] == "matched"
    assert result["prompt"] == "开始" + "填" * 19 + "结束"
    assert result["selected"]["measurement"]["input_tokens"] == 23
    assert len(result["probes"]) <= 32


def test_search_exhaustion_never_claims_impossible_or_emits_wrong_length():
    async def constant(prompt):
        return {"input_tokens": 2, "verification": "verified"}

    result = asyncio.run(fit_length(spec(max_probes=3), constant))
    assert result["status"] == "search_exhausted" and result["prompt"] is None
    assert len(result["probes"]) == 3
    assert "not_proof_of_impossibility" in result["reason"]


def test_character_cap_and_unknown_measurement_are_enforced():
    seen = []

    async def counter(prompt):
        seen.append(prompt)
        return {"input_tokens": len(prompt), "verification": "verified"}

    result = asyncio.run(fit_length(spec(max_characters=7), counter))
    assert result["status"] == "search_exhausted"
    assert max(map(len, seen)) <= 7

    async def unknown(prompt):
        return {"input_tokens": 23, "verification": "declared"}

    with pytest.raises(EvidenceError, match="unverified"):
        asyncio.run(fit_length(spec(), unknown))
