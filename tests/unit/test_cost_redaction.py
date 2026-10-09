"""Differential checks against the scalar algorithm frozen from e26d238."""

import random
from itertools import product

import pytest

from inferyard.evidence.storage import Redactor, json_bytes


def baseline_feed(secrets, pending, chunk, final):
    # Frozen minimal pure logic from e26d238 storage.StreamRedactor.feed.
    text = pending + chunk
    pending = ""
    result = []
    index = 0
    changed = False
    max_secret_length = max(map(len, secrets), default=0)
    if not secrets:
        return chunk, pending, changed
    while index < len(text):
        remaining_length = len(text) - index
        match = next((s for s in secrets if text.startswith(s, index)), None)
        if match:
            result.append("[REDACTED]")
            changed = True
            index += len(match)
        elif (
            not final
            and remaining_length < max_secret_length
            and any(
                remaining_length < len(secret)
                and all(
                    text[index + offset] == secret[offset] for offset in range(remaining_length)
                )
                for secret in secrets
            )
        ):
            pending = text[index:]
            break
        else:
            result.append(text[index])
            index += 1
    return "".join(result), pending, changed


def assert_chunks(secrets, chunks):
    redactor = Redactor(secrets)
    stream = redactor.stream()
    pending, changed = "", False
    for chunk, final in chunks:
        expected, pending, replaced = baseline_feed(redactor.secrets, pending, chunk, final)
        changed |= replaced
        assert stream.feed(chunk, final=final) == expected
        assert stream.pending == pending
        assert stream.changed == redactor.changed == changed


@pytest.mark.parametrize(
    "secrets,text",
    [
        (("abcdef", "ab", "bcd"), "zabcdefabc"),
        (("abcXYZ", "X"), "abcX"),
        (("aba", "bab", "ab"), "abababa"),
        (("aaaa", "aa"), "aaaaaaa"),
        (("秘密🔑", "🔑密"), "秘密🔑密秘密"),
        (("a\nb", "\nbc"), "za\nbc\na"),
        ((".*", "[x]", "\\"), ".*[x]\\."),
        (("REDACTED", "[REDACTED]"), "[REDACTED]"),
        (("secret",), "[REDACTED]"),
        ((), "秘密🔑"),
    ],
)
def test_every_chunk_partition(secrets, text):
    for cuts in product((False, True), repeat=max(0, len(text) - 1)):
        chunks, start = [("", False)], 0
        for end, cut in enumerate(cuts, 1):
            if cut:
                chunks.extend(((text[start:end], False), ("", False)))
                start = end
        chunks.append((text[start:], False))
        assert_chunks(secrets, [*chunks, ("", True), ("", True)])
        assert_chunks(secrets, [*chunks[:-1], (chunks[-1][0], True)])


def test_random_chunks_and_final_boundaries():
    rng = random.Random(2638)
    alphabet = "ab[]秘密🔑\n.*"
    for _ in range(1000):
        secrets = ["".join(rng.choices(alphabet, k=rng.randrange(1, 16))) for _ in range(5)]
        text = "".join(rng.choices([*alphabet, *secrets, "[REDACTED]"], k=40))
        chunks = []
        while text:
            size = rng.randrange(1, 24)
            chunks.append((text[:size], rng.random() < 0.1))
            text = text[size:]
        assert_chunks(secrets, [*chunks, ("", True)])


def test_prefix_not_emitted_before_later_complete_match():
    stream = Redactor(["abcXYZ", "X"]).stream()
    assert stream.feed("safe abcX") == "safe "
    assert stream.pending == "abcX"
    assert stream.feed("YZ") == "[REDACTED]"
    short = Redactor(["abcdef", "ab"]).stream()
    assert short.feed("abc") == "[REDACTED]c"
    assert short.pending == ""


@pytest.mark.parametrize("secrets", [(), ("absent",)])
def test_clean_container_isolation_and_tuple_conversion(secrets):
    shared = {"items": ["plain", (1, True, None)]}
    opaque = object()
    source = {"left": shared, "right": shared, "tuple": (shared,), "opaque": opaque}
    redactor = Redactor(secrets)
    cleaned = redactor.clean(source)
    assert cleaned == {
        "left": {"items": ["plain", [1, True, None]]},
        "right": {"items": ["plain", [1, True, None]]},
        "tuple": [{"items": ["plain", [1, True, None]]}],
        "opaque": opaque,
    }
    assert cleaned["left"] is not cleaned["right"]
    cleaned["left"]["items"][1].append(2)
    assert shared["items"][1] == (1, True, None)
    assert cleaned["right"]["items"][1] == [1, True, None]
    shared["items"].append("later")
    assert len(cleaned["tuple"][0]["items"]) == 2
    assert cleaned["opaque"] is opaque
    assert not redactor.changed


def test_clean_keys_marker_flags_and_serialized_output():
    redactor = Redactor(["secret"])
    result = redactor.clean({"secret-key": ("secret", {"x": "[REDACTED]"})})
    assert json_bytes(result) == b'{"[REDACTED]-key":["[REDACTED]",{"x":"[REDACTED]"}]}\n'
    assert redactor.changed
    redactor.secrets = ()
    assert redactor.clean(("plain",)) == ["plain"]
    assert redactor.changed
