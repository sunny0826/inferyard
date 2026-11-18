import asyncio
import json

import httpx
import pytest

from inferyard.adapters import engine_fit
from inferyard.adapters.engine_fit import FitClient, FitTransportError


def frame(content=None, finish=None, *, index=0):
    return {"choices": [{"index": index, "delta": {"content": content}, "finish_reason": finish}]}


def sse(*events):
    return "".join(
        f"data: {event if isinstance(event, str) else json.dumps(event, ensure_ascii=False)}\n\n"
        for event in events
    ).encode()


class Chunks(httpx.AsyncByteStream):
    def __init__(self, chunks, delay=0):
        self.chunks = chunks
        self.delay = delay
        self.closed = False

    async def __aiter__(self):
        for chunk in self.chunks:
            if self.delay:
                await asyncio.sleep(self.delay)
            yield chunk

    async def aclose(self):
        self.closed = True


def stream_response(events=None, *, stream=None):
    return httpx.Response(
        200,
        headers={"content-type": "text/event-stream; charset=utf-8"},
        stream=stream or Chunks([sse(*(events or []))]),
    )


def run_request(handler, method="complete", *, engine="vllm", timeout=1, **kwargs):
    async def run():
        async with FitClient(
            engine,
            "http://127.0.0.1:8000",
            "fixture-model",
            transport=httpx.MockTransport(handler),
            **kwargs,
        ) as client:
            if method == "complete":
                return await client.complete("fixture prompt", 16, timeout)
            return await getattr(client, method)()

    return asyncio.run(run())


@pytest.mark.parametrize("ids", [["fixture-model", "lora-model"], ["alias", "fixture-model"]])
def test_inspect_rejects_multiple_models_or_aliases_before_version_probe(ids):
    seen = []

    def handler(request):
        seen.append(request.url.path)
        return httpx.Response(200, json={"data": [{"id": model_id} for model_id in ids]})

    with pytest.raises(FitTransportError, match="^served_model_unverified$"):
        run_request(handler, "inspect")
    assert seen == ["/v1/models"]


@pytest.mark.parametrize(
    "usage,reason",
    [(None, "endpoint_usage_missing"), ({"completion_tokens": 1}, "endpoint_usage_incomplete")],
)
def test_missing_usage_is_null_with_reason_and_no_text_has_no_first_content(usage, reason):
    events = [frame(finish="length")]
    if usage is not None:
        events.append({"choices": [], "usage": usage})
    result = run_request(lambda _: stream_response([*events, "[DONE]"]))
    assert result["prompt_tokens"] is None
    assert result["usage_missing_reason"] == reason
    assert result["first_content_ms"] is None
    assert result["text"] == ""


@pytest.mark.parametrize(
    "origin",
    [
        "https://example.com",
        "http://192.168.1.3",
        "http://127.0.0.1/v1",
        "http://user:secret@localhost",
        "http://127.0.0.1?key=secret",
    ],
)
def test_non_loopback_origins_credentials_and_paths_are_rejected(origin):
    with pytest.raises(FitTransportError, match="^invalid_loopback_origin$"):
        FitClient("vllm", origin, "fixture-model")
