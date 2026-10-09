"""Single-entry inputs and wall-clock policy for the shared trial lifecycle."""

import asyncio
import time
from collections.abc import Callable
from dataclasses import dataclass

from inferyard.evidence.journal import TrialJournal


@dataclass(frozen=True)
class SingleProfile:
    # The single entry creates its journal and input/rerun snapshots before taking the lock.
    store: TrialJournal
    kind: str
    secret: str | None
    started_at: float
    allowed_seconds: float
    review: Callable
    sleep: Callable = asyncio.sleep
    monotonic: Callable = time.monotonic


class TrialDeadline:
    def __init__(self, started_at, allowed_seconds, *, profile=None):
        self.started_at, self.allowed_seconds = started_at, allowed_seconds
        self.profile = profile
        self.current = asyncio.current_task()
        self.prior_cancellations = self.current.cancelling()
        self.expired = False
        self.execution = None
        self.reason = "single_wall_budget_exhausted" if profile else "trial_wall_budget_exhausted"

    def expire(self):
        self.expired = True
        if self.execution is not None:
            self.execution.cancellation_reason = self.reason

    def check(self):
        clock = self.profile.monotonic if self.profile else time.monotonic
        if clock() - self.started_at >= self.allowed_seconds:
            self.expire()
            raise asyncio.CancelledError

    async def wait(self):
        clock = self.profile.monotonic if self.profile else time.monotonic
        sleep = self.profile.sleep if self.profile else asyncio.sleep
        await sleep(max(0, self.allowed_seconds - (clock() - self.started_at)))
        # Only single runs retain a user cancellation already draining at deadline expiry.
        if self.profile is None or self.current.cancelling() == self.prior_cancellations:
            self.expire()
            self.current.cancel()
