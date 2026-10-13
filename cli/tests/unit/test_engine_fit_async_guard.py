"""A slow native observer cannot postpone HTTP cancellation or outlive sealing."""

import asyncio
import gc
import threading

import pytest

from inferyard.adapters.engine_fit import FitTransportError
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.engine_fit import _guarded_completion


class SlowGuard:
    def __init__(self, *, fail=False):
        self.started = threading.Event()
        self.release = threading.Event()
        self.finished = threading.Event()
        self.fail = fail

    def check(self, phase):
        assert phase == "in_flight"
        self.started.set()
        try:
            assert self.release.wait(5), "test did not release its synthetic native read"
            if self.fail:
                raise PreflightError("synthetic_identity_changed")
        finally:
            self.finished.set()


@pytest.mark.parametrize("native_failure", [False, True])
def test_http_deadline_fires_while_native_observation_is_still_pending(native_failure):
    async def scenario():
        guard = SlowGuard(fail=native_failure)
        expired = asyncio.Event()
        unhandled = []
        asyncio.get_running_loop().set_exception_handler(
            lambda _, context: unhandled.append(context)
        )

        class Client:
            async def complete(self, prompt, tokens, timeout):
                try:
                    async with asyncio.timeout(timeout):
                        await asyncio.Event().wait()
                except TimeoutError as exc:
                    expired.set()
                    raise FitTransportError("request_timeout") from exc

        task = asyncio.create_task(
            _guarded_completion(
                Client(), "fixed", {"max_tokens": 1, "request_timeout_seconds": 0.8}, guard
            )
        )
        try:
            assert await asyncio.to_thread(guard.started.wait, 2)
            await asyncio.wait_for(expired.wait(), timeout=2)
            assert not guard.finished.is_set()
            assert not task.done()  # Evidence must wait for the one pending native read.
        finally:
            guard.release.set()
        expected = PreflightError if native_failure else FitTransportError
        with pytest.raises(expected):
            await task
        assert guard.finished.is_set()
        del task
        gc.collect()
        await asyncio.sleep(0)
        assert not unhandled

    asyncio.run(scenario())


@pytest.mark.parametrize("native_failure", [False, True])
def test_cancellation_closes_http_before_draining_observer(native_failure):
    async def scenario():
        guard = SlowGuard(fail=native_failure)
        http_cancelled = asyncio.Event()

        class Client:
            async def complete(self, prompt, tokens, timeout):
                try:
                    await asyncio.Event().wait()
                finally:
                    http_cancelled.set()

        task = asyncio.create_task(
            _guarded_completion(
                Client(), "fixed", {"max_tokens": 1, "request_timeout_seconds": 30}, guard
            )
        )
        try:
            assert await asyncio.to_thread(guard.started.wait, 2)
            task.cancel()
            await asyncio.wait_for(http_cancelled.wait(), timeout=2)
            assert not guard.finished.is_set()
            assert not task.done()
        finally:
            guard.release.set()
        with pytest.raises(asyncio.CancelledError):
            await task
        assert guard.finished.is_set()

    asyncio.run(scenario())
