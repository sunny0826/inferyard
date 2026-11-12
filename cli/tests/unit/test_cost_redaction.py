"""Credentials split across chunks or nested in JSON must remain redacted."""

from inferyard.contracts.validation import json_bytes
from inferyard.contracts.redaction import Redactor


def test_prefix_not_emitted_before_later_complete_match():
    stream = Redactor(["abcXYZ", "X"]).stream()
    assert stream.feed("safe abcX") == "safe "
    assert stream.feed("YZ") == "[REDACTED]"
    short = Redactor(["abcdef", "ab"]).stream()
    assert short.feed("abc") == "[REDACTED]c"


def test_keys_and_nested_values_are_redacted_in_serialized_output():
    redactor = Redactor(["secret"])
    result = redactor.clean({"secret-key": ("secret", {"x": "[REDACTED]"})})
    assert json_bytes(result) == b'{"[REDACTED]-key":["[REDACTED]",{"x":"[REDACTED]"}]}\n'
