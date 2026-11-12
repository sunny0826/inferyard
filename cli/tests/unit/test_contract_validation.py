"""Reject invalid measurements and budgets at actual document input boundaries."""

from copy import deepcopy

import pytest

from inferyard.config.loader import load_config
from inferyard.contracts.validation import validate_document
from inferyard.errors import ContractError


@pytest.mark.parametrize(
    "field,accepted,rejected",
    [("timeout_seconds", 0.5, 0), ("warmup_count", 3, 4)],
)
def test_config_rejects_zero_timeout_and_excess_warmup(config_path, tmp_path, field, accepted, rejected):
    bundle = config_path.parent.parent / "contracts/bundle.valid.json"
    text = config_path.read_text().replace("../contracts/bundle.valid.json", bundle.as_posix())
    candidate = tmp_path / "budget.toml"
    candidate.write_text(text + f"\n[execution]\n{field} = {accepted}\n")
    assert load_config(candidate).config.to_dict()["execution"][field] == accepted
    candidate.write_text(text + f"\n[execution]\n{field} = {rejected}\n")
    with pytest.raises(ContractError) as error:
        load_config(candidate)
    assert error.value.path.endswith(f".execution.{field}")


@pytest.mark.parametrize("key", ["secret\nvalue", "secret.value", "密钥", "a" * 65, "a\n"])
def test_untrusted_unknown_keys_keep_bounded_error_paths(key, wire_fixture):
    event = wire_fixture("event")
    event[key] = "fixture-secret-value"
    with pytest.raises(ContractError) as error:
        validate_document("event", event)
    assert (error.value.path, error.value.reason) == ("event.<unknown-field>", "unknown field")
    assert key not in str(error.value)
    assert "fixture-secret-value" not in str(error.value)


@pytest.mark.parametrize("value", [True, "1", float("nan"), float("inf"), -float("inf")])
def test_repeated_document_validation_rechecks_mutated_input_and_preserves_semantic_errors(
    wire_fixture, value,
):
    sample = wire_fixture("sample")
    saved = deepcopy(sample)
    validate_document("sample", sample)
    sample["value"] = value
    with pytest.raises(ContractError):
        validate_document("sample", sample)
    sample["value"] = saved["value"]
    sample["read_finished_ns"] = sample["read_started_ns"] - 1
    with pytest.raises(ContractError) as error:
        validate_document("sample", sample)
    assert error.value.path == "sample.read_finished_ns"
