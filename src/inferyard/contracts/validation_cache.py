"""Content-bound successful validations scoped to a single live command."""

import hashlib
from contextlib import contextmanager
from contextvars import ContextVar

_validated = ContextVar("validated_documents", default=None)


@contextmanager
def command_validation(*, enabled=True):
    token = _validated.set(set() if enabled else None)
    try:
        yield
    finally:
        _validated.reset(token)


def _plain(value):
    if type(value) is dict:
        return all(type(k) is str and _plain(v) for k, v in value.items())
    if type(value) is list:
        return all(_plain(v) for v in value)
    return type(value) in (str, int, float, bool, type(None))


def validation_key(kind, data):
    cache = _validated.get()
    if cache is None or kind not in ("config", "config_input", "bundle", "plan", "experiment"):
        return None, None
    # repr preserves numeric types and dictionary insertion order. Restrict it to
    # builtins so a custom repr can never confer trust on a different value.
    if not _plain(data):
        return None, None
    return cache, (kind, hashlib.sha256(repr(data).encode()).digest())
