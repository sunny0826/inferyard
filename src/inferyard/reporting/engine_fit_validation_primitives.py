"""Shared strict primitives for offline diagnostic evidence validation."""

import math
import re
from pathlib import PurePosixPath, PureWindowsPath

from inferyard.evidence.storage import EvidenceError


def require(condition, reason):
    if not condition:
        raise EvidenceError("engine_fit_" + reason)


def fields(value, names, reason):
    require(type(value) is dict and value.keys() == names, reason)


def text(value):
    return type(value) is str and bool(value.strip())


def number(value, *, integer=False):
    types = (int,) if integer else (int, float)
    return (
        type(value) in types
        and value >= 0
        and (value <= 2**63 - 1 if type(value) is int else math.isfinite(value))
    )


def digest(value):
    return type(value) is str and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def absolute(value):
    return text(value) and (
        PurePosixPath(value).is_absolute() or PureWindowsPath(value).is_absolute()
    )
