"""Logical time for the in-process, MockTransport overhead qualification scenario.

Native counters and evidence IO still run, but this fixture does not measure their
wall-clock cost. Production schedule/gap rejection is covered separately by
test_environment_schedule, test_environment_qualification and test_external_cpu.
"""

import asyncio
import importlib
import selectors
import sys
import time
from functools import partial

from inferyard.platforms.resources import ResourceSampler
from inferyard.runtime.safety_trace import SafetyTrace


def use_logical_time(monkeypatch, *, collector_pause=0):
    # Load default clock arguments before patching the shared time module.
    native = "macos" if sys.platform == "darwin" else "linux"
    importlib.import_module(f"inferyard.platforms.resources_{native}")
    now = time.monotonic_ns()

    def monotonic_ns():
        nonlocal now
        # Preserve ordering between synchronous reads without measuring host work.
        now += 1_000
        return now

    def monotonic():
        return monotonic_ns() / 1e9

    def loop_factory():
        selector = selectors.DefaultSelector()
        select = selector.select

        def logical_select(timeout=None):
            nonlocal now
            events = select(0)
            if not events and timeout is not None and timeout > 0:
                now += round(timeout * 1e9)
            return events

        monkeypatch.setattr(selector, "select", logical_select)
        return asyncio.SelectorEventLoop(selector)

    def sampler(store, config):
        value = ResourceSampler(store, config, clock=monotonic_ns)
        value.sensors.clock = monotonic_ns
        collect = value.collect
        paused = False

        def collect_with_host_pause(pid, ticks):
            nonlocal paused
            if collector_pause and not paused and value.phase == "formal":
                paused = True
                time.sleep(collector_pause)
            return collect(pid, ticks)

        value.collect = collect_with_host_pause
        return value

    monkeypatch.setattr(time, "monotonic", monotonic)
    monkeypatch.setattr(time, "monotonic_ns", monotonic_ns)
    monkeypatch.setattr("inferyard.platforms.resources.ResourceSampler", sampler)
    monkeypatch.setattr("inferyard.runtime.overhead_runner.ResourceSampler", sampler)
    monkeypatch.setattr(
        "inferyard.runtime.trial_runner.SafetyTrace", partial(SafetyTrace, clock=monotonic_ns)
    )
    return partial(asyncio.run, loop_factory=loop_factory)
