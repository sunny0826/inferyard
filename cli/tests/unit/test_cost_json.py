"""Strict JSON rejection and scalar-type compatibility for the pairs fast path."""

import pytest

from inferyard.contracts.validation import ContractError, _validate, strict_json_loads


@pytest.mark.parametrize(
    "text,reason",
    [
        ('{"a":1,"a":2}', "duplicate JSON key"),
        ('{"a":{"b":1,"b":2}}', "duplicate JSON key"),
        ('[{"a":1,"\\u0061":2}]', "duplicate JSON key"),
        ('{"秘密":1,"秘密":2}', "duplicate JSON key"),
        ('{"a":1,"a":NaN}', "non-finite JSON number"),
        ('{"a":1,"a":1e999}', "non-finite JSON number"),
        ("[NaN]", "non-finite JSON number"),
        ("Infinity", "non-finite JSON number"),
        ("-Infinity", "non-finite JSON number"),
        ("-1e9999", "non-finite JSON number"),
        ('{"a":}', "invalid JSON syntax or nesting"),
    ],
)
def test_exact_rejection(text, reason):
    with pytest.raises(ContractError) as caught:
        strict_json_loads(text)
    assert type(caught.value) is ContractError
    assert caught.value.path == "document"
    assert caught.value.reason == reason
    assert str(caught.value) == f"document: {reason}"


def test_nested_values_order_and_boolean_contract():
    value = strict_json_loads('{"z":[true,false,null,1,1.5,1e-9999],"a":{}}')
    assert list(value) == ["z", "a"]
    assert value == {"z": [True, False, None, 1, 1.5, 0.0], "a": {}}
    assert [type(item) for item in value["z"]] == [bool, bool, type(None), int, float, float]
    for kind in ("integer", "number"):
        with pytest.raises(ContractError, match="invalid type or non-finite number"):
            _validate(value["z"][0], {"type": kind}, "value")


def test_wide_object_keeps_every_pair():
    source = "{" + ",".join(f'"key{i}":{i}' for i in range(1000)) + "}"
    assert strict_json_loads(source) == {f"key{i}": i for i in range(1000)}


def test_decoder_recursion_error_keeps_contract(monkeypatch):
    def exhausted(*args, **kwargs):
        raise RecursionError("decoder nesting exhausted")

    monkeypatch.setattr("inferyard.contracts.validation.json.loads", exhausted)
    with pytest.raises(ContractError, match="^document: invalid JSON syntax or nesting$"):
        strict_json_loads("[]")
