"""Shared context speed wire contract regressions."""

from pathlib import Path

import pytest

from inferyard.contracts.context_speed import validate_response
from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import ContractError

GOLDEN = (
    Path(__file__).resolve().parents[3]
    / "studio/internal/inferyard/testdata/context_speed_contract.json"
)
CASES = strict_json_loads(GOLDEN.read_text())


@pytest.mark.parametrize("case", CASES, ids=lambda c: c["name"])
def test_shared_golden(case):
    if case["valid"]:
        validate_response(case["response"], case["exit_code"])
    else:
        with pytest.raises(ContractError):
            validate_response(case["response"], case["exit_code"])
