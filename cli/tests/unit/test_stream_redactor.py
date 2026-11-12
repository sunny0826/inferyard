"""Credentials split across output chunks must remain redacted."""

import pytest

from inferyard.contracts.redaction import Redactor


@pytest.mark.parametrize(
    "secrets,chunks,expected",
    [
        # A complete short secret is replaced before later chunks arrive.
        (("ab", "aba", "bab"), ["a", "b", "a"], "[REDACTED]a"),
        (("秘密🔑", "🔑密"), ["公开秘", "密", "🔑尾"], "公开[REDACTED]尾"),
        (("aaaa",), ["aa", "aaaa"], "[REDACTED]aa"),
        (("secret",), ["x" * 100_000 + "secretse", "cret"], "x" * 100_000 + "[REDACTED]" * 2),
        (("secret",), ["se"], "se"),
    ],
)
def test_overlaps_long_short_unicode_and_final_flush_match_old_stream(secrets, chunks, expected):
    stream = Redactor(secrets).stream()
    result = "".join(stream.feed(chunk) for chunk in chunks)
    result += stream.feed("", final=True)
    assert result == expected
