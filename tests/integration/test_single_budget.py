"""Frozen single-run deadline stops waits and requests, keeping cleanup evidence."""

import asyncio
from pathlib import Path
from types import SimpleNamespace

import pytest

import inferyard.runtime.lock as locking
import inferyard.runtime.runner as running
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import read_json, read_jsonl
from tests.integration.test_runner import scenario as runner_scenario

scenario = runner_scenario


class ControlledAsyncio:
    def __init__(self, expiry, *, baseline=False):
        self.expiry, self.baseline = expiry, baseline
        self.deadline_seconds = None

    def __getattr__(self, name):
        return getattr(asyncio, name)

    async def sleep(self, seconds):
        if seconds > 1:
            self.deadline_seconds = seconds
            await self.expiry.wait()
        elif self.baseline:
            self.expiry.set()
            await asyncio.Future()
        else:
            await asyncio.sleep(seconds)


@pytest.mark.parametrize(
    "stage", ["initial", "pre_request", "baseline", "generation", "generation_busy", "residual"]
)
def test_overall_budget_stops_waits_and_requests_and_releases_resources(
    scenario, monkeypatch, stage
):
    request, deps, calls, _ = scenario
    instances = []
    factory = deps.adapter

    async def run():
        expiry = asyncio.Event()
        clock = ControlledAsyncio(expiry, baseline=stage == "baseline")
        monkeypatch.setattr(running, "asyncio", clock)

        def adapter(*args, **kwargs):
            instance = factory(*args, **kwargs)
            instances.append(instance)
            wait_idle, generate = instance.wait_idle, instance.generate
            formal_waits = 0

            async def wait(*args, **kwargs):
                nonlocal formal_waits
                count = len(calls)
                if count == 5:
                    formal_waits += 1
                if stage == "generation_busy" and count == 6:
                    return False
                if (
                    (stage == "initial" and count == 0)
                    or (stage == "pre_request" and count == 5 and formal_waits == 2)
                    or (stage == "residual" and count == 6)
                ):
                    expiry.set()
                    await asyncio.Future()
                return await wait_idle(*args, **kwargs)

            async def generation(*args, **kwargs):
                if stage in ("generation", "generation_busy") and len(calls) == 5:
                    expiry.set()
                return await generate(*args, **kwargs)

            instance.wait_idle, instance.generate = wait, generation
            return instance

        deps.adapter = adapter
        code, result = await asyncio.wait_for(running.execute_async(request, deps), 5)
        frozen = read_json(Path(result.evidence_dir) / "plan.json")["experiment"]["budget"]
        assert 0 < clock.deadline_seconds <= frozen["max_wall_seconds"]
        assert not [
            t
            for t in asyncio.all_tasks()
            if t is not asyncio.current_task() and hasattr(t.get_coro(), "__qualname__")
        ]
        return code, result

    code, result = asyncio.run(run())
    assert code == 3, result
    assert "single_wall_budget_exhausted" in result.limitations
    root = Path(result.evidence_dir)
    data = read_trial(root)
    counts = data["summary"]["counts"]
    assert counts["planned"] == 3
    assert (
        sum(counts[key] for key in ("completed", "failed", "cancelled", "invalid", "not_executed"))
        == 3
    )
    assert (root / "manifest.json").exists()
    assert instances[0].client.is_closed
    assert read_json(locking.STATE_PATH)["dirty"] is (
        stage in ("initial", "generation_busy", "residual")
    )
    if stage in ("generation", "generation_busy"):
        assert counts["cancelled"] == 1 and counts["not_executed"] == 2
        assert data["requests"][0]["error_category"].startswith("single_wall_budget_exhausted")
        events, _ = read_jsonl(root / "events.jsonl")
        if stage == "generation":
            assert any(
                e["event_type"] == "idle_observed" and e["phase"] == "residual" for e in events
            )
    with locking.HostLock():
        pass


def test_user_cancel_during_generation_retains_reason_when_deadline_fires_in_drain(
    scenario, monkeypatch
):
    request, deps, calls, _ = scenario
    factory = deps.adapter

    async def run():
        expiry, draining = asyncio.Event(), asyncio.Event()
        monkeypatch.setattr(running, "asyncio", ControlledAsyncio(expiry))

        def adapter(*args, **kwargs):
            instance = factory(*args, **kwargs)
            wait_idle = instance.wait_idle

            async def wait(*args, **kwargs):
                if len(calls) == 6:
                    expiry.set()
                    draining.set()
                    await asyncio.sleep(0)
                return await wait_idle(*args, **kwargs)

            instance.wait_idle = wait
            return instance

        deps.adapter = adapter
        task = asyncio.create_task(running.execute_async(request, deps))
        while len(calls) < 6:
            await asyncio.sleep(0)
        task.cancel()
        code, result = await asyncio.wait_for(task, 5)
        assert draining.is_set()
        return code, result

    code, result = asyncio.run(run())
    assert code == 130 and "user_cancelled" in result.limitations
    data = read_trial(Path(result.evidence_dir))
    assert data["requests"][0]["error_category"].startswith("user_cancelled")
    assert not read_json(locking.STATE_PATH)["dirty"]


def test_synchronous_preflight_overrun_stops_before_first_request(scenario, monkeypatch):
    request, deps, calls, _ = scenario
    config, bundle = request.config.config.to_dict(), request.config.bundle.to_dict()
    budget = running.compile_single_plan(config, bundle)["experiment"]["budget"]["max_wall_seconds"]
    now = [100.0]
    monkeypatch.setattr(running, "time", SimpleNamespace(monotonic=lambda: now[0]))
    preflight = deps.preflight

    def slow_preflight(config):
        result = preflight(config)
        now[0] += budget + 1
        return result

    deps.preflight = slow_preflight
    code, result = asyncio.run(running.execute_async(request, deps))
    assert code == 3 and "single_wall_budget_exhausted" in result.limitations
    assert calls == []
    assert read_trial(Path(result.evidence_dir))["summary"]["counts"]["not_executed"] == 3
    with locking.HostLock():
        pass
