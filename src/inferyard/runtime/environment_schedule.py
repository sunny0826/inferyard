"""Independent one-second environment polling and declared resource cadence."""

import asyncio
import time

from inferyard.evidence.environment_projection import periodic_environment
from inferyard.platforms.identity import environment_snapshot


def advance(due, interval, now):
    due += interval
    if due < now:
        due += (int((now - due) // interval) + 1) * interval
    return due


async def run_sampler(sampler):
    loop = asyncio.get_running_loop()
    interval = sampler.config["telemetry"]["interval_ms"] / 1000
    due = environment_due = loop.time()
    try:
        while not sampler.stopped:
            actual = loop.time()
            resource_due = actual >= due
            if resource_due:
                schedule = {
                    "scheduled_ns": int(due * 1e9),
                    "actual_ns": int(actual * 1e9),
                    "late_ns": int(max(0, actual - due) * 1e9),
                }
                endpoint = sampler.config["endpoint"]
                for sample in sampler.collect(
                    endpoint["server_pid"], endpoint["process_start_ticks"]
                ):
                    sampler.store.sample(sample)
            if actual >= environment_due:
                observation = {
                    "monotonic_ns": time.monotonic_ns(),
                    "phase": sampler.phase,
                    "snapshot": periodic_environment(
                        environment_snapshot(
                            constants=getattr(sampler, "environment_constants", None)
                        )
                    ),
                }
                sampler.store.observation("environment.jsonl", observation)
                environment_due = advance(environment_due, 1, loop.time())
            sampler.store.flush_due()
            if resource_due:
                schedule["collector_work_ns"] = int((loop.time() - actual) * 1e9)
                sampler.store.observation("schedule.jsonl", schedule)
                due = advance(due, interval, loop.time())
            await sampler.wait(max(0, min(due, environment_due) - loop.time()))
    except Exception as exc:
        sampler.failure = exc
        raise
