"""Stopping wakes idle samplers and finishes a collection already started."""

import asyncio
from types import SimpleNamespace

import pytest

from inferyard.platforms import telemetry
from inferyard.runtime import environment_schedule


def make_sampler():
    observations, samples = [], []
    store = SimpleNamespace(
        sample=samples.append,
        observation=lambda name, row: observations.append((name, row)),
        flush_due=lambda: None,
    )
    sampler = telemetry.Sampler(
        store,
        {
            "telemetry": {"interval_ms": 60_000},
            "endpoint": {"server_pid": 1, "process_start_ticks": 2},
        },
    )
    return sampler, observations, samples


def test_stop_wakes_pending_wait_and_can_be_reset():
    async def scenario():
        sampler, _, _ = make_sampler()
        waiting = asyncio.create_task(sampler.wait(60))
        await asyncio.sleep(0)
        sampler.stopped = True
        await asyncio.wait_for(waiting, timeout=1)
        sampler.stopped = False
        waiting = asyncio.create_task(sampler.wait(60))
        await asyncio.sleep(0)
        assert not waiting.done()
        sampler.stopped = True
        await asyncio.wait_for(waiting, timeout=1)

    asyncio.run(scenario())


@pytest.mark.parametrize("module", [telemetry, environment_schedule])
def test_stop_finishes_started_collection_and_writes_schedule(monkeypatch, module):
    sampler, observations, samples = make_sampler()

    def collect(*args):
        yield {"sample": 1}
        sampler.stopped = True
        yield {"sample": 2}

    sampler.collect = collect
    monkeypatch.setattr(module, "environment_snapshot", lambda: {"platform": "fixture"})

    async def scenario():
        await asyncio.wait_for(
            sampler.run() if module is telemetry else module.run_sampler(sampler), timeout=1
        )

    asyncio.run(scenario())
    assert samples == [{"sample": 1}, {"sample": 2}]
    assert [name for name, _ in observations] == ["environment.jsonl", "schedule.jsonl"]
    assert sampler.failure is None


@pytest.mark.parametrize("module", [telemetry, environment_schedule])
def test_stop_does_not_hide_failure_from_started_read(monkeypatch, module):
    sampler, _, samples = make_sampler()
    failure = OSError("injected collection failure")

    def collect(*args):
        yield {"sample": 1}
        sampler.stopped = True
        raise failure

    sampler.collect = collect

    async def scenario():
        with pytest.raises(OSError) as caught:
            await (sampler.run() if module is telemetry else module.run_sampler(sampler))
        assert caught.value is failure

    asyncio.run(scenario())
    assert samples == [{"sample": 1}]
    assert sampler.failure is failure


def test_wait_cancellation_propagates():
    async def scenario():
        sampler, _, _ = make_sampler()
        waiting = asyncio.create_task(sampler.wait(60))
        await asyncio.sleep(0)
        waiting.cancel()
        with pytest.raises(asyncio.CancelledError):
            await waiting

    asyncio.run(scenario())
