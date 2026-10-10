"""Regression boundaries between single and batch execution profiles."""

import asyncio
from pathlib import Path
from types import SimpleNamespace

import pytest

from inferyard.runtime.runner import execute_async
from inferyard.runtime.trial_runner import run_trial
from tests.integration.test_runner import scenario as runner_scenario
from tests.integration.test_trial_runner import inputs

scenario = runner_scenario


@pytest.mark.parametrize("resource", ["disk", "memory"])
def test_batch_resource_guard_does_not_leak_into_single(scenario, monkeypatch, resource):
    import inferyard.runtime.trial_runner as trials

    request, deps, _, _ = scenario
    if resource == "disk":
        monkeypatch.setattr(trials.shutil, "disk_usage", lambda _: SimpleNamespace(free=0))
    else:
        monkeypatch.setattr(trials, "memory_available", lambda: 0)
    code, result = asyncio.run(execute_async(request, deps))
    assert code == 0, result
    plan, loaded, deps, output = inputs(scenario)
    code, _, root = asyncio.run(
        run_trial(
            plan, plan["trials"][0]["trial_id"], loaded, output, diagnostic=True, dependencies=deps
        )
    )
    assert code == 2
    from inferyard.evidence.storage import read_jsonl

    events, issues = read_jsonl(root / "events.jsonl")
    assert not issues
    assert events[-1]["data"]["reason"] == resource + "_safety_budget_reached"


@pytest.mark.parametrize("single", [True, False])
def test_profile_admission_order_and_artifact_boundaries(scenario, monkeypatch, single):
    import inferyard.runtime.runner as runner
    import inferyard.runtime.service_reuse as reuse
    import inferyard.runtime.trial_runner as trials

    request, deps, _, _ = scenario
    order = []
    original = reuse.snapshot

    def snapshot(*args, **kwargs):
        order.append("reuse")
        return original(*args, **kwargs)

    monkeypatch.setattr(reuse, "snapshot", snapshot)
    monkeypatch.setattr(runner, "require_review", lambda _: order.append("review"))
    monkeypatch.setattr(trials, "require_review", lambda _: order.append("review"))
    if single:
        code, result = asyncio.run(execute_async(request, deps))
        root = Path(result.evidence_dir)
    else:
        plan, loaded, deps, output = inputs(scenario)
        code, _, root = asyncio.run(
            run_trial(plan, plan["trials"][0]["trial_id"], loaded, output, dependencies=deps)
        )
    assert code == 0
    assert order == (["reuse", "review"] if single else ["review", "reuse"])
    files = {p.name for p in root.iterdir()}
    assert "input-provenance.json" not in files
    assert ("config.input.toml" in files) is single
    assert ("input-target-check.json" in files) is not single
    assert ("execution-budget.json" in files) is not single


@pytest.mark.parametrize("single", [True, False])
def test_finalization_preserves_existing_identity_and_sampler_shutdown(tmp_path, single):
    from inferyard.runtime.trial_finalization import finish_trial

    identity = tmp_path / "identity.json"
    identity.write_text('{"preserved":true}')
    snapshots, phases, stopped = [], [], []
    store = SimpleNamespace(
        path=tmp_path,
        sealed=False,
        run_id="fixture",
        snapshot=lambda name, value: snapshots.append(name),
        event=lambda *args: None,
        seal=lambda: None,
        close=lambda: None,
    )
    sampler = SimpleNamespace(stopped=False, set_phase=phases.append)

    async def sample():
        try:
            while not sampler.stopped:
                await asyncio.sleep(0)
        except asyncio.CancelledError:
            stopped.append("cancelled")
            raise
        else:
            stopped.append("returned")

    async def run():
        task = asyncio.create_task(sample())
        await asyncio.sleep(0)
        return await finish_trial(
            store,
            None,
            sampler,
            task,
            {},
            lambda: {},
            0,
            "plan_finished",
            lock=None,
            locked=False,
            safety=None,
            started_at=0,
            allowed_seconds=10,
            single=single,
        )

    assert asyncio.run(run()) == (0, "plan_finished")
    assert identity.read_text() == '{"preserved":true}'
    assert "identity.json" not in snapshots
    assert phases == ([] if single else ["finalizing"])
    assert stopped == (["cancelled"] if single else ["returned"])


@pytest.mark.parametrize("single", [True, False])
@pytest.mark.parametrize("failure", ["close", "seal"])
def test_finalization_error_mapping_and_lock_release(tmp_path, single, failure):
    from inferyard.evidence.storage import EvidenceError
    from inferyard.runtime.trial_finalization import finish_trial

    calls = []

    def seal():
        calls.append("seal")
        if failure == "seal":
            raise RuntimeError("synthetic seal failure")

    def close():
        calls.append("close")
        if failure == "close":
            raise EvidenceError("synthetic close failure")

    store = SimpleNamespace(
        path=tmp_path,
        sealed=False,
        run_id="fixture",
        snapshot=lambda *args: None,
        event=lambda *args: None,
        seal=seal,
        close=close,
    )
    lock = SimpleNamespace(__exit__=lambda *args: calls.append("unlock"))

    async def run():
        return await finish_trial(
            store,
            None,
            None,
            None,
            {},
            lambda: {},
            0,
            "plan_finished",
            lock=lock,
            locked=True,
            safety=None,
            started_at=0,
            allowed_seconds=10,
            single=single,
        )

    if failure == "close" and single:
        code, _ = asyncio.run(run())
        assert code == 4
    else:
        with pytest.raises(RuntimeError if failure == "seal" else EvidenceError):
            asyncio.run(run())
    # Preserve the old boundary even for unexpected exceptions: batch always releases,
    # single maps EvidenceError from close but lets an unexpected seal failure escape.
    assert calls == (["seal"] if single and failure == "seal" else ["seal", "close", "unlock"])
