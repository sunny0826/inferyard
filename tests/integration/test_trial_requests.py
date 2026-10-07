"""Actual Prism stream parser wired into durable request and host-lock boundaries."""

import asyncio
from pathlib import Path

import pytest

import inferyard.runtime.lock as locking
from inferyard.analysis.scoring import score_case
from inferyard.config.planning import compile_plan
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, Redactor, read_json
from inferyard.platforms.identity import PreflightError
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


async def all_cases(execution, bundle, calls):
    for i, case in enumerate(bundle["cases"]):
        await execution.one("formal", case["prompt"], index=i)


def test_streams_failures_and_no_implicit_retries(scenario):
    _, _, calls, settings = scenario
    settings["fail_index"] = 1
    data, reason = asyncio.run(pipeline(scenario, all_cases))
    assert reason == "plan_finished"
    assert len(calls) == 3
    assert [r["execution_state"] for r in data["requests"]] == ["completed", "failed", "completed"]
    assert data["summary"]["completeness"] == "complete"
    row = data["requests"][0]
    assert row["arrival_capture"] == {"source": "decoded_delta", "streaming": True}
    assert row["block_arrivals"][0]["monotonic_ns"] == row["t_first_content_ns"]
    assert data["summary"]["performance"][row["category"]]["metrics"]["L03"]["sample_count"] >= 1
    assert not read_json(locking.STATE_PATH)["dirty"]


def test_cancel_terminates_once_and_does_not_start_next_case(scenario):
    async def cancel(execution, bundle, calls):
        task = asyncio.create_task(execution.one("formal", bundle["cases"][0]["prompt"], index=0))
        while not calls:
            await asyncio.sleep(0.001)
        task.cancel()
        await task

    data, reason = asyncio.run(pipeline(scenario, cancel))
    assert reason == "user_cancelled"
    assert data["summary"]["counts"]["cancelled"] == 1
    assert data["summary"]["counts"]["not_executed"] == 2
    assert len(scenario[2]) == 1
    assert not read_json(locking.STATE_PATH)["dirty"]


def test_scorer_exception_keeps_all_model_completions(scenario):
    def broken(*args):
        raise RuntimeError("synthetic error")

    data, reason = asyncio.run(pipeline(scenario, all_cases, scorer=broken))
    assert reason == "plan_finished"
    assert data["summary"]["counts"]["completed"] == 3
    assert data["summary"]["counts"]["failed"] == 0
    assert data["requests"][0]["score"]["reason"] == "scorer_exception"
    assert len(scenario[2]) == 3


def test_unknown_residual_keeps_dirty_and_blocks_following_requests(scenario):
    _, deps, calls, _ = scenario
    factory = deps.adapter

    def adapter(*args, **kwargs):
        instance = factory(*args, **kwargs)

        async def idle(*args, **kwargs):
            return not calls

        instance.wait_idle = idle
        return instance

    deps.adapter = adapter
    data, reason = asyncio.run(pipeline(scenario, all_cases))
    assert reason == "service_stop_unconfirmed"
    assert len(calls) == 1
    assert data["summary"]["counts"]["not_executed"] == 2
    assert read_json(locking.STATE_PATH)["dirty"]


def test_concurrent_invocation_rejected_without_second_dispatch(scenario):
    async def overlap(execution, bundle, calls):
        task = asyncio.create_task(execution.one("formal", bundle["cases"][0]["prompt"], index=0))
        while not calls:
            await asyncio.sleep(0.001)
        with pytest.raises(PreflightError, match="concurrent"):
            await execution.one("formal", bundle["cases"][1]["prompt"], index=1)
        await task

    data, _ = asyncio.run(pipeline(scenario, overlap))
    assert len(scenario[2]) == 1
    assert data["summary"]["counts"]["completed"] == 1


def test_io_failure_leaves_invalid_not_model_failed(scenario, monkeypatch):
    original = TrialJournal.event

    def failing(self, kind, *args, **kwargs):
        if kind == "content":
            raise EvidenceError("synthetic_io_failure")
        return original(self, kind, *args, **kwargs)

    monkeypatch.setattr(TrialJournal, "event", failing)
    data, _ = asyncio.run(pipeline(scenario, all_cases))
    assert data["summary"]["counts"]["invalid"] == 1
    assert data["summary"]["counts"]["failed"] == 0
    assert data["summary"]["counts"]["not_executed"] == 2
    assert read_json(locking.STATE_PATH)["dirty"]


@pytest.mark.parametrize("field,value", [("category", "qa"), ("scorer_sha256", "f" * 64)])
def test_invalid_scorer_identity_stops_without_fabricating_score(scenario, field, value):
    def broken(case, answer, policy):
        score = score_case(case, answer, policy)
        score[field] = value
        return score

    data, reason = asyncio.run(pipeline(scenario, all_cases, scorer=broken))
    assert reason == "scorer_identity_mismatch"
    assert data["requests"][0]["execution_state"] == "completed"
    assert data["requests"][0]["score"] is None
    assert data["summary"]["counts"]["not_executed"] == 2


def test_redacted_answer_never_scored_and_secret_absent_from_artifacts(scenario):
    secret = "synthetic-sensitive-answer-93a"
    scenario[3]["answer"] = secret
    data, reason = asyncio.run(pipeline(scenario, all_cases, secret=secret))
    assert reason == "plan_finished"
    assert data["requests"][0]["score"]["reason"] == "redacted_scoring_input"
    root = Path(scenario[0].config.config.to_dict()["output"]["root"]) / data["run"]["run_id"]
    assert all(secret.encode() not in p.read_bytes() for p in root.iterdir())


@pytest.mark.parametrize("delay_stage", ["snapshot", "observation"])
def test_duration_admission_rechecks_deadline_after_durable_preparation(scenario, delay_stage):
    import time

    async def crosses_deadline(execution, bundle, calls):
        original = execution.store.snapshot

        def slow_snapshot(name, value):
            result = original(name, value)
            if name.endswith(".request.json") and delay_stage == "snapshot":
                time.sleep(0.03)
            return result

        execution.store.snapshot = slow_snapshot
        if delay_stage == "observation":
            execution.sampler.before_request = lambda: time.sleep(0.03)
        terminal = await execution.one(
            "formal",
            bundle["cases"][0]["prompt"],
            index=0,
            admission_deadline_ns=time.monotonic_ns() + 10_000_000,
        )
        assert terminal is None
        assert execution.next_formal == 0 and not calls
        assert not read_json(locking.STATE_PATH)["dirty"]

    data, _ = asyncio.run(pipeline(scenario, crosses_deadline))
    assert all(r["execution_state"] == "not_executed" for r in data["requests"])
