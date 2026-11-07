"""First body text per request, aligned across engines (ADR 056).

mlx-lm streams the leading `<think>` tag as text and llama-server parses it away,
so the first output differs by engine. The first non-blank text after that tag is
the same output position on both sides.
"""

THINK_OPEN = "<think>"


def _has_body(text):
    stripped = text.lstrip()
    if THINK_OPEN.startswith(stripped):  # Nothing yet, or a tag split across chunks.
        return False
    return bool(stripped.removeprefix(THINK_OPEN).strip())


def first_body_times(events):
    """Map request_id to the monotonic_ns of the event that completes the first body text."""
    texts, found = {}, {}
    for event in events:
        key = event.get("request_id")
        if event["event_type"] not in ("content", "reasoning") or key in found:
            continue
        texts[key] = texts.get(key, "") + event["data"]["text"]
        if _has_body(texts[key]):
            found[key] = event["monotonic_ns"]
    return found


def attach_first_body(data, events):
    found = first_body_times(events)
    data["requests"] = [
        {**row, "t_first_body_ns": found.get(row["request_id"])} for row in data["requests"]
    ]
