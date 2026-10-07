"""Bounded matching and streaming compatibility without wall-time benchmarks."""

from itertools import product

import pytest

from inferyard.evidence.storage import Redactor


def reference_feed(secrets, pending, chunk, final):
    text, result = pending + chunk, []
    index = 0
    while index < len(text):
        remaining = text[index:]
        match = next((secret for secret in secrets if remaining.startswith(secret)), None)
        if match:
            result.append("[REDACTED]")
            index += len(match)
        elif not final and any(secret.startswith(remaining) for secret in secrets):
            return "".join(result), remaining
        else:
            result.append(text[index])
            index += 1
    return "".join(result), ""


@pytest.mark.parametrize("secrets", [("ab", "aba", "bab"), ("秘密🔑", "🔑密"), ("aaaa",)])
def test_overlaps_long_short_unicode_and_final_flush_match_old_stream(secrets):
    alphabet = "ab" if secrets[0][0] in "ab" else "秘密🔑"
    for letters in product(alphabet, repeat=5):
        text = "".join(letters)
        for split in range(len(text) + 1):
            redactor = Redactor(secrets)
            stream, pending = redactor.stream(), ""
            changed = False
            for chunk, final in ((text[:split], False), (text[split:], False), ("", True)):
                expected, pending = reference_feed(redactor.secrets, pending, chunk, final)
                changed |= "[REDACTED]" in expected
                assert stream.feed(chunk, final=final) == expected
                assert stream.pending == pending
            assert stream.changed == redactor.changed == changed


def test_no_secret_returns_chunk_without_character_work():
    class Untouchable(str):
        def __len__(self):
            raise AssertionError("no-secret stream must not scan the chunk")

        def __getitem__(self, key):
            raise AssertionError("no-secret stream must not copy suffixes")

    stream = Redactor().stream()
    chunk = Untouchable("中文🔑" * 100_000)
    assert stream.feed(chunk) is chunk
    assert stream.feed(chunk, final=True) is chunk
    assert not stream.changed and not stream.redactor.changed


def test_secret_matching_never_copies_unbounded_suffixes():
    class CheckedText(str):
        def __radd__(self, other):
            return self if not other else CheckedText(other + str(self))

        def __getitem__(self, key):
            if isinstance(key, slice):
                start, stop, step = key.indices(len(self))
                assert len(range(start, stop, step)) < len("secret")
            return super().__getitem__(key)

    redactor = Redactor(["secret"])
    stream = redactor.stream()
    assert stream.feed(CheckedText("x" * 100_000 + "secretse")) == "x" * 100_000 + "[REDACTED]"
    assert stream.pending == "se"
    assert stream.feed("cret", final=True) == "[REDACTED]"
    assert stream.changed and redactor.changed
