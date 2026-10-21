import asyncio
import json

import httpx
import pytest

from inferyard.adapters.local_engines.protocol import FAMILIES
from inferyard.contracts.schemas_observation import OBSERVATION
from inferyard.contracts.validation import _validate
from inferyard.platforms.identity import PreflightError


@pytest.mark.parametrize("family", FAMILIES)
@pytest.mark.parametrize("stream", [False, True])
def test_formal_generation_and_queue_scope(rig, family, stream):
    async def run():
        r = rig(family)
        a = r.adapter
        try:
            props = await a.verify_properties(r.config)
            assert props["observation"]["definition"] == "local-engine-observation.v1"
            _validate(props["observation"], OBSERVATION, "observation")
            if family == "lmstudio":
                assert props["observation"]["endpoints"]["lms:ps"]["status_code"] is None
            observations = []
            assert await a.wait_ready(observe=lambda _, data: observations.append(data))
            events = []
            result = await a.generate(
                a.request_body(r.config, "hello", stream=stream),
                1,
                lambda kind, data, now: events.append((kind, data)),
            )
            assert result["execution_state"] == "completed"
            assert result["content"] == "ok"
            assert result["completion_tokens"] == 1
            assert not a.queue_ready
            assert await a.wait_ready(observe=lambda _, data: observations.append(data))
            assert a.queue_ready
            assert all(item["engine_internal_drain"] is None for item in observations)
            assert all(
                item["signals"]["queue_snapshot"]["observation"]["idle"] for item in observations
            )
            assert r.posts == [
                {
                    "model": "fixture",
                    "messages": [{"role": "user", "content": "hello"}],
                    "temperature": 0,
                    "max_tokens": 32,
                    "stream": stream,
                    **({"stream_options": {"include_usage": True}} if stream else {}),
                }
            ]
            assert "native_wire" in {event[0] for event in events}
            assert (await a.token_budget(r.config, "hello"))["input_tokens"] is None
            effective = await a.verify_effective(r.config, None, a.last_state)
            assert all(p["effective"] is None for p in effective["parameters"].values())
        finally:
            await a.close()

    asyncio.run(run())


@pytest.mark.parametrize("family", FAMILIES)
@pytest.mark.parametrize("failure", ["timeout", "http", "eof", "cancel"])
def test_failure_never_becomes_clean_when_queue_zero(rig, family, failure):
    async def response(request):
        if failure in ("timeout", "cancel"):
            await asyncio.sleep(10)
        if failure == "http":
            return httpx.Response(500)
        return httpx.Response(
            200,
            text='data: {"choices":[{"index":0,"delta":{"content":"x"},"finish_reason":null}]}\n\n',
        )

    async def run():
        r = rig(family, completion=response)
        a = r.adapter
        try:
            await a.verify_properties(r.config)
            assert await a.wait_ready()
            task = asyncio.create_task(a.generate(a.request_body(r.config, "hello"), 0.01))
            if failure == "cancel":
                await asyncio.sleep(0.001)
                task.cancel()
                with pytest.raises(asyncio.CancelledError):
                    await task
            else:
                result = await task
                assert result["execution_state"] == "failed"
            assert a.native_uncertain
            observed = []
            assert not await a.wait_ready(observe=lambda _, data: observed.append(data))
            assert observed[-1]["signals"]["queue_snapshot"]["observation"]["idle"]
            assert not a.queue_ready
            with pytest.raises(PreflightError, match="queue_admission"):
                await a.generate(a.request_body(r.config, "never"), 1)
            assert len(r.posts) == 1
        finally:
            await a.close()

    asyncio.run(run())


@pytest.mark.parametrize("family", FAMILIES[:-1])
@pytest.mark.parametrize("bad", ["missing", "duplicate", "negative", "nan", "label_gap"])
def test_metrics_cannot_admit_malformed_queue(rig, family, bad):
    async def run():
        r = rig(family)
        a = r.adapter
        try:
            await a.verify_properties(r.config)
            first, *rest = r.metrics.splitlines()
            r.metrics = {
                "missing": "",
                "duplicate": r.metrics + first + "\n",
                "negative": r.metrics.replace(" 0", " -1", 1),
                "nan": r.metrics.replace(" 0", " NaN", 1),
                "label_gap": first.replace(" 0", '{model="other"} 0') + "\n" + "\n".join(rest),
            }[bad]
            observed = []
            assert not await a.wait_ready(observe=lambda _, data: observed.append(data))
            assert observed[-1]["signals"]["queue_snapshot"]["missing_reason"]
            assert not r.posts
        finally:
            await a.close()

    asyncio.run(run())


@pytest.mark.parametrize(
    "field,value",
    [
        ("local_engine_protocol", "mlx-lm.v1"),
        ("local_engine_source_sha256", "0" * 64),
        ("generation_queue_source_sha256", "0" * 64),
        ("synthetic", True),
        ("max_model_len", True),
        ("max_model_len", 1024),
        ("version", "unknown"),
    ],
)
def test_mlx_formal_protocol_binding(rig, field, value):
    async def run():
        r = rig("mlx-lm")
        r.version[field] = value
        try:
            with pytest.raises(PreflightError, match="discovery_failed"):
                await r.adapter.verify_properties(r.config)
            assert not r.posts
        finally:
            await r.adapter.close()

    asyncio.run(run())


def test_identity_change_blocks_without_post(rig):
    async def run():
        r = rig()
        try:
            await r.adapter.verify_properties(r.config)
            r.binding["start_ticks"] += 1
            assert not await r.adapter.wait_ready()
            assert r.adapter.native_uncertain
            assert not r.posts
        finally:
            await r.adapter.close()

    asyncio.run(run())


@pytest.mark.parametrize("stream", [False, True])
def test_missing_usage_is_null_with_reason(rig, stream):
    async def response(request):
        if not json.loads(request.content)["stream"]:
            return httpx.Response(
                200,
                json={
                    "choices": [{"index": 0, "message": {"content": "ok"}, "finish_reason": "stop"}]
                },
            )
        return httpx.Response(
            200,
            text=(
                'data: {"choices":[{"index":0,"delta":{"content":"ok"},'
                '"finish_reason":"stop"}]}\n\ndata: [DONE]\n\n'
            ),
        )

    async def run():
        r = rig(completion=response)
        a = r.adapter
        try:
            await a.verify_properties(r.config)
            await a.wait_ready()
            events = []
            result = await a.generate(
                a.request_body(r.config, "hello", stream=stream),
                1,
                lambda kind, data, now: events.append((kind, data)),
            )
            assert result["execution_state"] == "completed"
            assert result["completion_tokens"] is None
            usage = next(data for kind, data in events if kind == "lab_usage")
            assert usage["usage_missing_reasons"]["completion_tokens"]
        finally:
            await a.close()

    asyncio.run(run())


def test_http_context_close_failure_is_not_clean(rig):
    class BrokenClose(httpx.AsyncByteStream):
        async def __aiter__(self):
            yield b'{"choices":[{"index":0,"message":{"content":"ok"},"finish_reason":"stop"}]}'

        async def aclose(self):
            raise httpx.ReadError("close failed")

    async def response(request):
        return httpx.Response(200, stream=BrokenClose())

    async def run():
        r = rig(completion=response)
        try:
            await r.adapter.verify_properties(r.config)
            assert await r.adapter.wait_ready()
            result = await r.adapter.generate(
                r.adapter.request_body(r.config, "hi", stream=False), 1
            )
            assert result["execution_state"] == "failed"
            assert r.adapter.native_uncertain
            assert not await r.adapter.wait_ready()
        finally:
            await r.adapter.close()

    asyncio.run(run())


def test_successful_http_then_observer_failure_stays_uncertain(rig):
    async def run():
        r = rig()
        try:
            await r.adapter.verify_properties(r.config)
            assert await r.adapter.wait_ready()
            assert (await r.adapter.generate(r.adapter.request_body(r.config, "hi"), 1))[
                "execution_state"
            ] == "completed"
            original = r.metrics
            r.metrics = ""
            assert not await r.adapter.wait_ready()
            r.metrics = original
            assert not await r.adapter.wait_ready()
            assert r.adapter.native_uncertain
        finally:
            await r.adapter.close()

    asyncio.run(run())


def test_lmstudio_invalid_count_blocks_admission(rig):
    async def run():
        r = rig("lmstudio")
        try:
            await r.adapter.verify_properties(r.config)
            r.observer.queued = True
            assert not await r.adapter.wait_ready()
            assert not r.posts
        finally:
            await r.adapter.close()

    asyncio.run(run())
