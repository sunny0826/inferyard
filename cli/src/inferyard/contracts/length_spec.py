"""Frozen prompt-length scan specification; structural rules only."""

from jsonschema import Draft202012Validator

from inferyard.errors import EvidenceError


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
