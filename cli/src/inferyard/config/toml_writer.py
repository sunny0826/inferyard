"""Preserve nested TOML declarations, with a round-trip check before publication."""

import json
import re
import tomllib


def toml_key(key):
    return key if re.fullmatch(r"[A-Za-z0-9_-]+", key) else toml_value(key)


def toml_value(value):
    if isinstance(value, dict):
        return "{" + ", ".join(f"{toml_key(k)} = {toml_value(v)}" for k, v in value.items()) + "}"
    if isinstance(value, list):
        return "[" + ", ".join(toml_value(item) for item in value) + "]"
    return json.dumps(value, ensure_ascii=False, allow_nan=False).replace("\x7f", r"\u007f")


def table_lines(path, values):
    lines = ["", "[" + ".".join(toml_key(key) for key in path) + "]"]
    lines.extend(
        f"{toml_key(key)} = {toml_value(value)}"
        for key, value in values.items()
        if not isinstance(value, dict)
    )
    for key, value in values.items():
        if isinstance(value, dict):
            lines.extend(table_lines((*path, key), value))
    return lines


def render(
    config, *, comment="Unbound candidate; bind an externally started service.", writer=table_lines
):
    lines = ["# " + comment, "schema_version = 3"]
    for section, values in config.items():
        if isinstance(values, dict):
            lines.extend(writer((section,), values))
    content = "\n".join(lines) + "\n"
    if tomllib.loads(content) != config:
        raise ValueError("configuration serialization mismatch")
    return content
