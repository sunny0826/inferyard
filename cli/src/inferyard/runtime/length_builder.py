"""Bounded measured search; non-monotonic tokenizers never imply impossibility."""

import hashlib

from jsonschema import Draft202012Validator

from inferyard.evidence.storage import EvidenceError

SPEC = {
    "type": "object",
    "additionalProperties": False,
    "required": [
        "prefix",
        "suffix",
        "padding_unit",
        "target_tokens",
        "tolerance_tokens",
        "max_repetitions",
        "max_probes",
        "max_characters",
        "max_wall_seconds",
    ],
    "properties": {
        **{
            key: {"type": "string", "maxLength": 65536}
            for key in ("prefix", "suffix", "padding_unit")
        },
        "target_tokens": {"type": "integer", "minimum": 1, "maximum": 1000000},
        "tolerance_tokens": {"type": "integer", "minimum": 0, "maximum": 100000},
        "max_repetitions": {"type": "integer", "minimum": 0, "maximum": 100000},
        "max_probes": {"type": "integer", "minimum": 1, "maximum": 256},
        "max_characters": {"type": "integer", "minimum": 1, "maximum": 65536},
        "max_wall_seconds": {"type": "integer", "minimum": 1, "maximum": 600},
    },
}


def validate_spec(spec):
    if not Draft202012Validator(SPEC).is_valid(spec):
        raise EvidenceError("length_spec_invalid")
    if not spec["padding_unit"] or spec["tolerance_tokens"] >= spec["target_tokens"]:
        raise EvidenceError("length_spec_invalid_padding_or_tolerance")
    if len(spec["prefix"]) + len(spec["suffix"]) > spec["max_characters"]:
        raise EvidenceError("length_base_text_exceeds_character_budget")


async def fit_length(spec, counter, *, observe=lambda record: None):
    validate_spec(spec)
    limit = min(
        spec["max_repetitions"],
        (spec["max_characters"] - len(spec["prefix"]) - len(spec["suffix"]))
        // len(spec["padding_unit"]),
    )
    tried, trace = set(), []
    low, high, candidate = 0, None, 0
    while len(trace) < spec["max_probes"]:
        if candidate in tried or not 0 <= candidate <= limit:
            break
        tried.add(candidate)
        prompt = spec["prefix"] + spec["padding_unit"] * candidate + spec["suffix"]
        measurement = await counter(prompt)
        count = measurement.get("input_tokens")
        if type(count) is not int or count < 0 or measurement.get("verification") != "verified":
            raise EvidenceError("length_counter_unverified")
        record = {
            "repetitions": candidate,
            "prompt_sha256": hashlib.sha256(prompt.encode()).hexdigest(),
            "measurement": measurement,
        }
        trace.append(record)
        observe(record)
        if abs(count - spec["target_tokens"]) <= spec["tolerance_tokens"]:
            return {"status": "matched", "prompt": prompt, "selected": record, "probes": trace}
        if count < spec["target_tokens"]:
            low = candidate
            candidate = (low + high) // 2 if high is not None else min(limit, max(1, candidate * 2))
        else:
            high = candidate
            candidate = (low + high) // 2
    return {
        "status": "search_exhausted",
        "prompt": None,
        "selected": None,
        "probes": trace,
        "reason": "bounded_search_did_not_find_match_not_proof_of_impossibility",
    }
