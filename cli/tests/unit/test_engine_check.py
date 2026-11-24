"""Shared CLI/Studio engine_check wire contract schema verification."""

import json
from pathlib import Path

import jsonschema
import pytest

from inferyard.contracts.schemas import export_schema


def test_contract_golden_samples_match_the_schema():
    samples = json.loads(
        (
            Path(__file__).resolve().parents[3]
            / "studio"
            / "internal"
            / "inferyard"
            / "testdata"
            / "engine_check_contract.json"
        ).read_text()
    )
    assert samples
    for sample in samples:
        if sample["valid"]:
            jsonschema.validate(sample["response"], export_schema("engine_check"))
        else:
            with pytest.raises(jsonschema.ValidationError):
                jsonschema.validate(sample["response"], export_schema("engine_check"))
