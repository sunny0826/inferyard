"""Successful review decisions owned by one command, never persisted."""

from contextlib import contextmanager
from contextvars import ContextVar

_reviews: ContextVar[set[str] | None] = ContextVar("bundle_reviews", default=None)


@contextmanager
def command_reviews():
    token = _reviews.set(set())
    try:
        yield
    finally:
        _reviews.reset(token)


def review_cache():
    return _reviews.get()
