"""Strict JSON rejection and scalar-type compatibility for the pairs fast path."""

import pytest

from inferyard.contracts.validation import strict_json_loads
from inferyard.errors import ContractError


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
    assert caught.value.path == "document"
    assert caught.value.reason == reason
    assert str(caught.value) == f"document: {reason}"
