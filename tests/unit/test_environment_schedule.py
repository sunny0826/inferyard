"""Slow resource cadence must not slow environment polling or invent missed samples."""

import asyncio
from types import SimpleNamespace

import pytest

from inferyard.platforms.telemetry import Sampler
from inferyard.runtime import environment_schedule
from inferyard.runtime.environment_observer import EnvironmentObserver


def test_independent_cadences_and_missed_deadlines(monkeypatch):
    clock = [0.0]
    resources, environment, schedules = [], [], []
    sampler = Sampler(
        store=None,
        config={
            "telemetry": {"interval_ms": 2000},
            "endpoint": {"server_pid": 12, "process_start_ticks": 34},
        },
    )
    sampler.set_phase("formal")

    def collect(*args):
        resources.append(clock[0])
        if clock[0] == 2:
            clock[0] = 5.5  # delayed reader: no fabricated observations for 3/4/5 seconds
        return []

    def observe(name, data):
        (environment if name == "environment.jsonl" else schedules).append(data)

    async def sleep(seconds):
        clock[0] += seconds
        if clock[0] >= 8:
            sampler.stopped = True

    sampler.collect = collect
    sampler.store = SimpleNamespace(
        sample=lambda _: None, observation=observe, flush_due=lambda: None
    )
    monkeypatch.setattr(
        environment_schedule.asyncio,
        "get_running_loop",
        lambda: SimpleNamespace(time=lambda: clock[0]),
    )
    monkeypatch.setattr(environment_schedule.asyncio, "sleep", sleep)
    monkeypatch.setattr(environment_schedule.time, "monotonic_ns", lambda: int(clock[0] * 1e9))
    monkeypatch.setattr(environment_schedule, "environment_snapshot", lambda: {"epp": "observed"})
    asyncio.run(EnvironmentObserver.run(sampler))
    assert resources == [0, 2, 6]
    assert [r["monotonic_ns"] / 1e9 for r in environment] == [0, 1, 5.5, 6, 7]
    assert [r["scheduled_ns"] / 1e9 for r in schedules] == [0, 2, 6]
    assert schedules[1]["collector_work_ns"] == 3_500_000_000
    assert sampler.failure is None
    assert not hasattr(sampler, "environment")
    assert not hasattr(sampler, "schedule")
    assert len(environment) == 5 and len(schedules) == 3
    assert all("queue_depth" not in row for row in schedules)


def test_observer_error_is_preserved(monkeypatch):
    sampler = SimpleNamespace(
        config={"telemetry": {"interval_ms": 2000}}, stopped=False, phase="formal", failure=None
    )
    sampler.config["endpoint"] = {"server_pid": 1, "process_start_ticks": 2}
    sampler.collect = lambda *a: []
    failure = OSError("injected sensor failure")

    def fail():
        raise failure

    monkeypatch.setattr(environment_schedule, "environment_snapshot", fail)
    with pytest.raises(OSError):
        asyncio.run(EnvironmentObserver.run(sampler))
    assert sampler.failure is failure
