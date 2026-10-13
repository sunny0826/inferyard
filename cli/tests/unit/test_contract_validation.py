"""Strict rejection paths retained by the allocation-free validator dispatch."""

from copy import deepcopy

import pytest

from inferyard.contracts.validation import ContractError, _validate, validate_document


class IntegerSubclass(int):
    pass


class DictSubclass(dict):
    pass


class ListSubclass(list):
    pass


@pytest.mark.parametrize(
    "kind,value",
    [
        ("object", {}),
        ("array", []),
        ("string", ""),
        ("integer", 0),
        ("number", -1),
        ("number", 1.5),
        ("number", -0.0),
        ("boolean", False),
        ("null", None),
    ],
)
def test_exact_json_types_remain_accepted(kind, value):
    _validate(value, {"type": kind}, "record.value")


@pytest.mark.parametrize(
    "kind,value",
    [
        ("object", DictSubclass()),
        ("array", ListSubclass()),
        ("string", 0),
        ("integer", True),
        ("integer", 1.0),
        ("integer", IntegerSubclass(1)),
        ("number", True),
        ("number", IntegerSubclass(1)),
        ("number", float("nan")),
        ("number", float("inf")),
        ("number", -float("inf")),
        ("boolean", 0),
        ("null", False),
    ],
)
def test_python_subclasses_booleans_and_nonfinite_numbers_remain_rejected(kind, value):
    with pytest.raises(ContractError) as error:
        _validate(value, {"type": kind}, "record.value")
    assert (error.value.path, error.value.reason) == (
        "record.value",
        "invalid type or non-finite number",
    )


@pytest.mark.parametrize("value", [None, -1, 1.5])
@pytest.mark.parametrize("kinds", [["number", "null"], ["null", "number"]])
def test_nullable_type_order_does_not_change_acceptance(value, kinds):
    _validate(value, {"type": kinds}, "record.value")


@pytest.mark.parametrize("value", [True, "1", float("nan"), float("inf")])
def test_nullable_numbers_do_not_hide_invalid_numeric_values(value):
    with pytest.raises(ContractError, match="invalid type or non-finite number"):
        _validate(value, {"type": ["number", "null"]}, "record.value")


@pytest.mark.parametrize(
    "keyword,limit,valid,invalid",
    [
        ("minimum", 0, 0, -0.5),
        ("maximum", 0, 0, 0.5),
        ("exclusiveMinimum", 0, 0.5, -0.0),
        ("exclusiveMaximum", 0, -0.5, 0),
    ],
)
def test_numeric_boundaries_preserve_inclusive_and_exclusive_errors(keyword, limit, valid, invalid):
    spec = {"type": "number", keyword: limit}
    _validate(valid, spec, "record.value")
    with pytest.raises(ContractError) as error:
        _validate(invalid, spec, "record.value")
    assert (error.value.path, error.value.reason) == ("record.value", "violates " + keyword)


def test_simultaneous_numeric_failures_keep_original_diagnostic_priority():
    with pytest.raises(ContractError) as error:
        _validate(0, {"type": "number", "minimum": 1, "maximum": -1}, "record.value")
    assert error.value.reason == "violates minimum"


@pytest.mark.parametrize("key", ["secret\nvalue", "secret.value", "密钥", "a" * 65, "a\n"])
def test_untrusted_unknown_keys_keep_bounded_error_paths(key, wire_fixture):
    event = wire_fixture("event")
    event[key] = "fixture-secret-value"
    with pytest.raises(ContractError) as error:
        validate_document("event", event)
    assert (error.value.path, error.value.reason) == ("event.<unknown-field>", "unknown field")
    assert key not in str(error.value)
    assert "fixture-secret-value" not in str(error.value)


def test_safe_field_name_at_length_limit_keeps_precise_error_path(wire_fixture):
    event = wire_fixture("event")
    key = "a" * 64
    event[key] = "rejected-value"
    with pytest.raises(ContractError) as error:
        validate_document("event", event)
    assert error.value.path == "event." + key
    assert "rejected-value" not in str(error.value)


def test_required_fields_are_checked_before_unrecognized_fields(wire_fixture):
    event = wire_fixture("event")
    del event["phase"]
    event["extra"] = 1
    with pytest.raises(ContractError) as error:
        validate_document("event", event)
    assert (error.value.path, error.value.reason) == ("event.phase", "required field missing")


def test_repeated_document_validation_rechecks_mutated_input_and_preserves_semantic_errors(
    wire_fixture,
):
    sample = wire_fixture("sample")
    saved = deepcopy(sample)
    validate_document("sample", sample)
    assert sample == saved
    sample["value"] = True
    with pytest.raises(ContractError) as error:
        validate_document("sample", sample)
    assert (error.value.path, error.value.reason) == (
        "sample",
        "does not match the allowed types or variants",
    )
    sample["value"] = saved["value"]
    sample["read_finished_ns"] = sample["read_started_ns"] - 1
    with pytest.raises(ContractError) as error:
        validate_document("sample", sample)
    assert error.value.path == "sample.read_finished_ns"
