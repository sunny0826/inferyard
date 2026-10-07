import asyncio
import sys

import pytest

from inferyard.evidence.storage import read_json, verify_manifest
from inferyard.extensions import independent_guard, total_control_runtime
from inferyard.extensions.workflow import guarded
from tests.unit.test_total_observer_control import packet


def test_guarded_request_cancels_and_joins_owned_task():
    async def check():
        stop = asyncio.Event()
        closed = []

        async def request():
            try:
                await asyncio.sleep(60)
            finally:
                closed.append(True)

        task = asyncio.create_task(guarded(request(), stop))
        await asyncio.sleep(0)
        stop.set()
        with pytest.raises(asyncio.CancelledError):
            await task
        assert closed == [True]

    asyncio.run(check())


@pytest.mark.skipif(sys.platform != "linux", reason="Linux fork-based independent guardian")
def test_independent_guard_is_another_process_and_records_final_boundary(
    tmp_path, config_path, monkeypatch
):
    import os

    from inferyard.config.loader import load_config

    class Safety:
        def __init__(self, *args):
            self.last = {}

        def check(self, **kwargs):
            self.last = {
                "environment": dict.fromkeys(
                    ("platform", "kernel", "cpu_model", "ac_online", "governor", "epp"), "fixture"
                )
            }

    monkeypatch.setattr(independent_guard, "SafetyGuard", Safety)
    guard = independent_guard.IndependentGuard(
        tmp_path / "guard.jsonl",
        load_config(config_path).config.to_dict(),
        policy={**independent_guard.POLICY, "interval_seconds": 0.01},
    )
    guard.start()
    assert guard.process.pid != os.getpid() and not guard.stopped()
    guard.close()
    proof = guard.evidence()
    assert len(proof["samples"]) >= 2
    assert proof["samples"][0]["monotonic_ns"] < proof["samples"][-1]["monotonic_ns"]
    assert guard.process.exitcode == 0


def test_total_runtime_uses_same_workload_and_seals_full_observer_arms(
    tmp_path, config_path, monkeypatch
):
    from inferyard.config.loader import load_config

    loaded = load_config(config_path)
    config = loaded.config.to_dict()
    bundle = loaded.bundle.to_dict()
    spec, _, _ = packet()
    spec["case_ids"] = [bundle["cases"][0]["case_id"]]

    class Sampler:
        def __init__(self, store, config):
            self.store = store
            self.stopped = False

        async def run(self):
            while True:
                self.store.observation("schedule.jsonl", {"fixture_periodic": True})
                await asyncio.sleep(0.001)

        def set_phase(self, *args):
            pass

        def before_request(self):
            self.store.observation("schedule.jsonl", {"fixture_before": True})

        def boundary(self, capture):
            self.store.observation("schedule.jsonl", {"fixture_boundary": capture})

    class Safety:
        def __init__(self, *args):
            self.last = None

        def check(self, **kwargs):
            self.last = {"fixture_safe": True}

    class Adapter:
        record = None

        def __init__(self):
            self.calls = []

        async def infer(self, case, slot):
            self.calls.append(case)
            await asyncio.sleep(0.002)
            return {"state": "completed", "answer": "fixture", "usage": {"completion_tokens": 1}}

        async def wait_idle(self, *args):
            return True

    class Guard:
        policy = independent_guard.POLICY
        clock_id = "fixture-clock"

        def stopped(self):
            return False

    monkeypatch.setattr(total_control_runtime, "ResourceSampler", Sampler)
    monkeypatch.setattr(total_control_runtime, "SafetyGuard", Safety)
    adapter = Adapter()
    arms = asyncio.run(
        total_control_runtime.execute_total(
            spec, adapter, tmp_path, config, bundle, Guard(), lambda a: None
        )
    )
    assert [a["mode"] for a in arms] == ["off", "on", "on", "off"]
    assert len(adapter.calls) == 4 * (config["execution"]["warmup_count"] + 1)
    children = list((tmp_path / "observer-arms").iterdir())
    assert len(children) == 2
    for child in children:
        assert not verify_manifest(child)
        assert not (child / "requests.jsonl").exists()
        assert read_json(child / "config.frozen.json") == config
        assert (child / "schedule.jsonl").stat().st_size > 0
        assert (child / "safety-checks.jsonl").stat().st_size > 0
