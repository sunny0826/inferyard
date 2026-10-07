"""Shared explicit comparison condition helpers."""

from __future__ import annotations


def _get(data, path):
    value = data
    for key in path.split("."):
        if not isinstance(value, dict) or key not in value:
            return None
        value = value[key]
    return value


def _known(value):
    if value is None or value == "unknown" or value == "":
        return False
    if isinstance(value, dict):
        return bool(value) and all(_known(v) for v in value.values())
    if isinstance(value, list):
        return all(_known(v) for v in value)
    return True


def comparable_arguments(args):
    location = {"-m", "--model", "--host", "--port", "-a", "--alias"}
    result = []
    skip = False
    for argument in args:
        if skip:
            skip = False
        elif argument in location:
            skip = True
        elif argument.split("=", 1)[0] in location:
            continue
        else:
            result.append(argument)
    return result
