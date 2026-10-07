"""Duration admission at the actual Prism coroutine start, with durable evidence."""

import asyncio
import time
from contextlib import asynccontextmanager
from types import SimpleNamespace

import httpx
import pytest

import inferyard.runtime.lock as locking
import inferyard.runtime.request_execution as execution_module
from inferyard.adapters.prism import PrismAdapter
from inferyard.config.loader import load_config
from inferyard.config.planning import compile_plan
from inferyard.evidence.event_ledger import reduce_events
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, read_json, read_jsonl
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.request_execution import TrialRequests
from tests.unit.test_phase2_contracts import experiment


class Clock:
    now = 1_000

    def __call__(self):
        return self.now

    def set(self, now):
        self.now = now


class Sampler:
    def __init__(self):
        self.phases = []
        self.boundaries = []

    def set_phase(self, phase, request_id):
        self.phases.append((phase, request_id))

    def boundary(self, name):
        self.boundaries.append(name)


@pytest.fixture
def pipeline(tmp_path, monkeypatch, config_path):
    clock = Clock()
    monkeypatch.setattr(time, "monotonic_ns", clock)
    monkeypatch.setattr(locking, "LEGACY_ROOT", None)
    monkeypatch.setattr(locking, "LOCK_PATH", tmp_path / "host.lock")
    monkeypatch.setattr(locking, "STATE_PATH", tmp_path / "host.state.json")

    @asynccontextmanager
    async def create(*, post=None):
        loaded = load_config(config_path)
        config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
        config["execution"]["timeout_seconds"] = 1
        source = experiment()
        workload = source["workloads"][0]
        workload.update(purpose="stability", repeats=1, timeout_seconds=1)
        workload["protocol"] = {
            "kind": "duration",
            "case_ids": [bundle["cases"][0]["case_id"]],
            "duration_seconds": 1,
            "window_seconds": 1,
            "max_requests": 5,
            "min_completed_per_case_per_window": 1,
            "drain_timeout_seconds": 1,
        }
        plan = compile_plan(source)
        store = TrialJournal(
            tmp_path / "runs",
            plan,
            plan["trials"][0]["trial_id"],
            config,
            bundle,
            diagnostic=True,
        )
        left, deadline = clock.now, clock.now + 10**9
        store.event(
            "duration_started",
            "formal",
            None,
            {
                "start_ns": left,
                "admission_deadline_ns": deadline,
                "drain_deadline_ns": deadline + 10**9,
                "request_limit": 5,
            },
            monotonic_ns=left,
        )
        posts, idle_dirty = [], []
        with locking.HostLock() as lock:

            async def handler(request):
                if request.method == "GET":
                    assert request.url.path == "/slots"
                    idle_dirty.append(bool(lock.state and lock.state["dirty"]))
                    return httpx.Response(200, json=[{"id": 0, "is_processing": False}])
                assert request.url.path == "/v1/chat/completions"
                posts.append(request)
                if post is not None:
                    return await post(request)
                clock.now += 10
                return httpx.Response(
                    200,
                    json={
                        "choices": [{"message": {"content": "北京"}, "finish_reason": "stop"}],
                        "usage": {"completion_tokens": 2},
                    },
                )

            adapter = PrismAdapter(
                config["endpoint"]["url"], transport=httpx.MockTransport(handler)
            )
            sampler = Sampler()
            sampler_task = asyncio.create_task(asyncio.Event().wait())
            requests = TrialRequests(
                store,
                config,
                bundle,
                adapter,
                sampler,
                sampler_task,
                lock,
                lambda *args: None,
                [],
            )

            def events(kind):
                store.flush()
                return [
                    e for e in read_jsonl(store.path / "events.jsonl")[0] if e["event_type"] == kind
                ]

            def finish(reason="duration_elapsed", returned=0, completed=0):
                if reason == "duration_elapsed":
                    clock.now = max(clock.now, deadline)
                store.event(
                    "duration_closed",
                    "formal",
                    None,
                    {
                        "closed_ns": clock.now,
                        "returned_requests": returned,
                        "completed_requests": completed,
                        "reason": "duration_elapsed"
                        if reason == "duration_elapsed"
                        else "interrupted",
                    },
                )
                store.event("run_stopped", "finalizing", None, {"reason": reason})
                store.seal()
                return read_trial(store.path)

            context = SimpleNamespace(
                clock=clock,
                deadline=deadline,
                requests=requests,
                adapter=adapter,
                sampler=sampler,
                lock=lock,
                store=store,
                posts=posts,
                idle_dirty=idle_dirty,
                prompt=bundle["cases"][0]["prompt"],
                events=events,
                finish=finish,
            )
            try:
                yield context
            finally:
                sampler_task.cancel()
                await asyncio.gather(sampler_task, return_exceptions=True)
                await adapter.close()
                store.close()

    return create


@pytest.mark.parametrize("offset", [0, 1])
def test_queued_generation_at_or_after_deadline_has_no_attempt(pipeline, offset):
    async def run():
        async with pipeline() as ctx:
            ctx.clock.set(ctx.deadline - 1)
            # This runs after control-layer preparation, before the new generation task.
            ctx.sampler.before_request = lambda: asyncio.get_running_loop().call_soon(
                ctx.clock.set, ctx.deadline + offset
            )
            terminal = await ctx.requests.one(
                "formal",
                ctx.prompt,
                index=0,
                stream=False,
                admission_deadline_ns=ctx.deadline,
            )
            assert terminal is None and ctx.adapter.last_state is None
            assert not ctx.posts and not ctx.events("request_started")
            assert not ctx.events("request_finished")
            assert ctx.requests.next_formal == 0 and not ctx.requests.formal_started
            assert ctx.idle_dirty == [False, True]
            assert not read_json(locking.STATE_PATH)["dirty"]
            assert ctx.sampler.phases[-1] == ("residual", None)
            assert ctx.finish()["requests"] == []

    asyncio.run(run())


def test_admitted_start_and_send_share_sample_and_include_durable_write_cost(pipeline):
    async def run():
        async with pipeline() as ctx:
            send = ctx.deadline - 1
            ctx.clock.set(send)
            event = ctx.store.event

            def slow_start(kind, *args, **kwargs):
                value = event(kind, *args, **kwargs)
                if kind == "request_started":
                    ctx.clock.now += 20
                return value

            ctx.store.event = slow_start
            terminal = await ctx.requests.one(
                "formal",
                ctx.prompt,
                index=0,
                stream=False,
                admission_deadline_ns=ctx.deadline,
            )
            assert len(ctx.posts) == 1
            assert ctx.events("request_started")[0]["monotonic_ns"] == terminal["t_send_ns"] == send
            assert terminal["t_terminal_ns"] - send == 30
            assert ctx.requests.next_formal == 1 and ctx.requests.formal_started
            data = ctx.finish(returned=1, completed=1)
            assert data["summary"]["duration"]["admitted_requests"] == 1
            assert data["summary"]["counts"]["completed"] == 1
            assert data["requests"][0]["t_send_ns"] == send

    asyncio.run(run())


def test_cancel_after_admission_keeps_cancelled_terminal(pipeline):
    async def run():
        posted = asyncio.Event()

        async def blocked_post(request):
            posted.set()
            await asyncio.Event().wait()

        async with pipeline(post=blocked_post) as ctx:
            send = ctx.clock.now
            task = asyncio.create_task(
                ctx.requests.one(
                    "formal",
                    ctx.prompt,
                    index=0,
                    stream=False,
                    admission_deadline_ns=ctx.deadline,
                )
            )
            await posted.wait()
            task.cancel()
            with pytest.raises(asyncio.CancelledError):
                await task
            terminal = ctx.events("request_finished")[0]["data"]
            assert terminal["execution_state"] == "cancelled" and terminal["t_send_ns"] == send
            assert len(ctx.posts) == 1 and ctx.requests.next_formal == 1
            assert ctx.idle_dirty == [False, True] and not ctx.lock.state["dirty"]
            assert ctx.finish("interrupted")["summary"]["counts"]["cancelled"] == 1

    asyncio.run(run())


def test_cancel_queued_generation_has_no_orphan_terminal(pipeline, monkeypatch):
    async def run():
        async with pipeline() as ctx:
            create_task = asyncio.create_task

            def cancel_before_run(coro):
                task = create_task(coro)
                task.cancel()
                return task

            monkeypatch.setattr(execution_module.asyncio, "create_task", cancel_before_run)
            with pytest.raises(asyncio.CancelledError):
                await ctx.requests.one(
                    "formal",
                    ctx.prompt,
                    index=0,
                    stream=False,
                    admission_deadline_ns=ctx.deadline,
                )
            assert not ctx.posts and ctx.adapter.last_state is None
            assert not ctx.events("request_started") and not ctx.events("request_finished")
            assert ctx.requests.next_formal == 0 and not ctx.requests.formal_started
            assert ctx.idle_dirty == [False, True] and not ctx.lock.state["dirty"]
            assert ctx.finish("interrupted")["requests"] == []

    asyncio.run(run())


@pytest.mark.parametrize("cancel_outer", [False, True])
def test_callback_write_failure_never_sends_or_cleans_dirty(pipeline, cancel_outer):
    async def run():
        async with pipeline() as ctx:
            event = ctx.store.event
            outer = asyncio.current_task()

            def fail_start(kind, *args, **kwargs):
                if kind == "request_started":
                    if cancel_outer:
                        asyncio.get_running_loop().call_soon(outer.cancel)
                    raise EvidenceError("synthetic_start_write_failure")
                return event(kind, *args, **kwargs)

            ctx.store.event = fail_start
            with pytest.raises(EvidenceError, match="synthetic_start_write_failure"):
                await ctx.requests.one(
                    "formal",
                    ctx.prompt,
                    index=0,
                    stream=False,
                    admission_deadline_ns=ctx.deadline,
                )
            assert not ctx.posts and ctx.adapter.last_state is None
            assert ctx.requests.next_formal == 0 and not ctx.requests.formal_started
            assert not ctx.events("request_started") and not ctx.events("request_finished")
            assert read_json(locking.STATE_PATH)["dirty"]
            assert ctx.idle_dirty == [False]
            assert ctx.finish("tool_error")["requests"] == []

    asyncio.run(run())


def test_declined_request_cannot_clear_dirty_without_idle(pipeline, monkeypatch):
    async def run():
        async with pipeline() as ctx:
            wait_idle = ctx.adapter.wait_idle

            async def missing_residual_idle(*args, **kwargs):
                if ctx.lock.state and ctx.lock.state["dirty"]:
                    return False
                return await wait_idle(*args, **kwargs)

            monkeypatch.setattr(ctx.adapter, "wait_idle", missing_residual_idle)
            ctx.sampler.before_request = lambda: asyncio.get_running_loop().call_soon(
                ctx.clock.set, ctx.deadline
            )
            with pytest.raises(PreflightError, match="service_stop_unconfirmed"):
                await ctx.requests.one(
                    "formal",
                    ctx.prompt,
                    index=0,
                    stream=False,
                    admission_deadline_ns=ctx.deadline,
                )
            assert not ctx.posts and read_json(locking.STATE_PATH)["dirty"]
            assert ctx.finish("service_stop_unconfirmed")["requests"] == []

    asyncio.run(run())


@pytest.mark.parametrize("duration", [False, True])
def test_missing_terminal_is_tool_failure_not_silent_decline(pipeline, monkeypatch, duration):
    async def run():
        async with pipeline() as ctx:
            generate = ctx.adapter.generate

            async def omit_terminal(*args, **kwargs):
                await generate(*args, **kwargs)
                return None

            monkeypatch.setattr(ctx.adapter, "generate", omit_terminal)
            with pytest.raises(EvidenceError, match="request_terminal_missing"):
                await ctx.requests.one(
                    "formal",
                    ctx.prompt,
                    index=0,
                    stream=False,
                    admission_deadline_ns=ctx.deadline if duration else None,
                )
            assert len(ctx.posts) == 1 and ctx.requests.next_formal == 1
            assert read_json(locking.STATE_PATH)["dirty"]
            assert not ctx.events("request_finished")
            ctx.store.event("run_stopped", "finalizing", None, {"reason": "tool_error"})
            ctx.store.seal()
            rows, _, _ = reduce_events(
                read_jsonl(ctx.store.path / "events.jsonl")[0],
                read_json(ctx.store.path / "run.json"),
                read_json(ctx.store.path / "selection.json"),
                ctx.requests.cases,
                duration=ctx.store.duration_protocol,
            )
            assert len(rows) == 1 and rows[0]["execution_state"] == "invalid"

    asyncio.run(run())


def test_generate_default_interface_and_declined_state_are_compatible():
    async def run():
        posts, samples = [], []

        def handler(request):
            posts.append(request)
            return httpx.Response(500)

        adapter = PrismAdapter("http://127.0.0.1:8080", transport=httpx.MockTransport(handler))
        try:
            assert (await adapter.generate({"stream": False}, 1))["error_category"] == "http_error"
            assert adapter.last_state is not None and len(posts) == 1
            assert (
                await adapter.generate(
                    {"stream": False}, 1, before_send=lambda sample: samples.append(sample) or False
                )
                is None
            )
            assert adapter.last_state is None and len(samples) == 1 and len(posts) == 1
        finally:
            await adapter.close()

    asyncio.run(run())
