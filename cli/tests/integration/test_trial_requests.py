"""Actual Prism stream parser wired into durable request and host-lock boundaries."""

import asyncio
from pathlib import Path

import pytest

import inferyard.runtime.lock as locking
from inferyard.analysis.scoring import score_case
from inferyard.config.planning import compile_plan
from inferyard.errors import EvidenceError, PreflightError
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import Redactor, json_bytes, read_json, read_jsonl
from inferyard.runtime.request_execution import TrialRequests
from tests.integration.test_runner import scenario as runner_scenario
from tests.unit.test_phase2_contracts import experiment

scenario = runner_scenario


async def pipeline(scenario, action, *, scorer=score_case, secret=None):
    request, deps, calls, settings = scenario
    config, bundle = request.config.config.to_dict(), request.config.bundle.to_dict()
    source = experiment()
    source["workloads"][0]["protocol"]["case_ids"] = [c["case_id"] for c in bundle["cases"]]
    source["workloads"][0]["timeout_seconds"] = config["execution"]["timeout_seconds"]
    source["budget"]["max_wall_seconds"] = 100_000
    plan = compile_plan(source)
    store = TrialJournal(
        Path(config["output"]["root"]),
        plan,
        plan["trials"][0]["trial_id"],
        config,
        bundle,
        redactor=Redactor([secret] if secret else []),
    )
    adapter = deps.adapter(config["endpoint"]["url"], secret=secret)
    sampler = deps.sampler(store, config)
    sampler_task = asyncio.create_task(sampler.run())
    reason = "plan_finished"
    try:
        with deps.lock() as lock:
            execution = TrialRequests(
                store,
                config,
                bundle,
                adapter,
                sampler,
                sampler_task,
                lock,
                deps.guard,
                [],
                scorer=scorer,
            )
            try:
                await action(execution, bundle, calls)
            except asyncio.CancelledError:
                reason = "user_cancelled"
            except (PreflightError, EvidenceError) as exc:
                reason = str(exc)
            finally:
                sampler.stopped = True
                await sampler_task
                store.event("run_stopped", "finalizing", None, {"reason": reason})
                store.seal()
    finally:
        await adapter.close()
        store.close()
    return read_trial(store.path), reason


@pytest.mark.parametrize("delay_stage", ["prepared", "observation"])
def test_duration_admission_rechecks_deadline_after_durable_preparation(
    scenario, delay_stage, tmp_path
):
    import time

    async def crosses_deadline(execution, bundle, calls):
        original = execution.store.prepare_request

        def slow_prepare(*args):
            result = original(*args)
            if delay_stage == "prepared":
                time.sleep(0.03)
            return result

        execution.store.prepare_request = slow_prepare
        if delay_stage == "observation":
            execution.sampler.before_request = lambda: time.sleep(0.03)
        terminal = await execution.one(
            "formal",
            bundle["cases"][0]["prompt"],
            index=0,
            admission_deadline_ns=time.monotonic_ns() + 10_000_000,
        )
        assert terminal is None
        assert not calls
        assert not read_json(locking.STATE_PATH)["dirty"]

    data, _ = asyncio.run(pipeline(scenario, crosses_deadline))
    assert all(r["execution_state"] == "not_executed" for r in data["requests"])
    root = Path(scenario[0].config.config.to_dict()["output"]["root"]) / data["run"]["run_id"]
    prepared, body_tails = read_jsonl(root / "request-bodies.jsonl")
    events, event_tails = read_jsonl(root / "events.jsonl")
    assert len(prepared) == 1 and not body_tails and not event_tails
    assert not any(e["event_type"] == "request_started" for e in events)
    (tmp_path / "runtime-receipt.json").write_bytes(
        json_bytes(
            {
                "synthetic": True,
                "transport": "httpx.MockTransport; no TCP/model",
                "entry": "TrialRequests.one actual runtime admission; no CLI mapping",
                "delay_stage": delay_stage,
                "received": scenario[2],
                "prepared": prepared,
                "events": events,
                "host_state": read_json(locking.STATE_PATH),
                "counts": data["summary"]["counts"],
            }
        )
    )
