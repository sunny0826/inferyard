import asyncio
import json
from pathlib import Path

import httpx
import pytest

from inferyard.adapters.prism import PrismAdapter, ResponseState, SSEDecoder, StreamProtocol
from inferyard.evidence.storage import Redactor
from inferyard.platforms.identity import PreflightError


def frame(delta=None, reason=None, usage=None):
    value = {"choices": [{"index": 0, "delta": delta or {}, "finish_reason": reason}]}
    if usage is not None:
        value["usage"] = usage
    return json.dumps(value, ensure_ascii=False)


def test_f05_exact_timeline_reasoning_and_usage():
    events = []
    state = ResponseState(0)
    protocol = StreamProtocol(state, Redactor(), lambda *event: events.append(event))
    protocol.accept(frame({"role": "assistant"}), 100_000_000)
    protocol.accept(frame({"content": ""}), 150_000_000)
    protocol.accept(frame({"reasoning_content": "想"}), 200_000_000)
    protocol.accept(frame({"content": "北京"}), 300_000_000)
    protocol.accept(frame({"content": "多个token"}), 700_000_000)
    protocol.accept(frame(reason="stop"), 1_100_000_000)
    protocol.accept(json.dumps({"choices": [], "usage": {"completion_tokens": 12}}), 1_150_000_000)
    protocol.accept("[DONE]", 1_200_000_000)
    result = state.terminal(1_200_000_000)
    assert result["execution_state"] == "completed"
    assert result["t_first_content_ns"] == 200_000_000
    assert result["t_first_answer_ns"] == 300_000_000
    assert result["completion_tokens"] / (result["t_terminal_ns"] / 1e9) == 10
    assert result["content"] == "北京多个token"
    assert result["reasoning"] == "想"


def test_stream_secret_never_reconstructs_from_persisted_chunks():
    emitted = []
    state = ResponseState(0)
    protocol = StreamProtocol(
        state, Redactor(["fixture-secret-7f39"]), lambda kind, data, t: emitted.append(data)
    )
    for i, part in enumerate(["fixture-", "secret-", "7f39"]):
        protocol.accept(frame({"content": part}), i + 1)
    protocol.accept(frame(reason="stop"), 4)
    protocol.accept("[DONE]", 5)
    assert state.redacted
    assert "fixture-secret-7f39" not in json.dumps(emitted)
    assert "".join(state.content) == "[REDACTED]"
    assert state.t_first_content_ns == 1


def test_utf8_sse_crlf_multiline_and_comments():
    payload = (
        ": heartbeat\r\n\r\ndata: "
        + frame({"content": "北京"})
        + "\r\n\r\n"
        + "data: [DONE]\r\n\r\n"
    ).encode()
    decoder = SSEDecoder()
    result = []
    for byte in payload:
        result.extend(decoder.feed(bytes([byte])))
    decoder.finish()
    assert json.loads(result[0])["choices"][0]["delta"]["content"] == "北京"
    assert result[1] == "[DONE]"


@pytest.mark.parametrize(
    "reason,done,expected",
    [
        ("stop", True, "completed"),
        ("length", True, "completed"),
        ("stop", False, "failed"),
        (None, True, "failed"),
        ("unknown", True, "failed"),
    ],
)
def test_finish_semantics_and_missing_done(reason, done, expected):
    async def run():
        data = "data: " + frame({"content": "北京"}, reason) + "\n\n"
        if done:
            data += "data: [DONE]\n\n"
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(
                lambda request: httpx.Response(200, content=data.encode())
            ),
        )
        result = await adapter.generate({"stream": True}, 1)
        await adapter.close()
        return result

    result = asyncio.run(run())
    assert result["execution_state"] == expected
    assert result["completion_tokens"] is None
    assert result["budget_exhausted"] is (reason == "length")


def test_total_deadline_not_renewed_by_content():
    class NeverEnds(httpx.AsyncByteStream):
        async def __aiter__(self):
            while True:
                await asyncio.sleep(0.01)
                yield ("data: " + frame({"content": "x"}) + "\n\n").encode()

    async def run():
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(lambda request: httpx.Response(200, stream=NeverEnds())),
        )
        result = await adapter.generate({"stream": True}, 0.08)
        await adapter.close()
        return result

    result = asyncio.run(run())
    assert result["error_category"] == "total_timeout"
    elapsed = (result["t_terminal_ns"] - result["t_send_ns"]) / 1e9
    assert 0.075 <= elapsed < 0.33


def test_redirect_is_not_followed_and_headers_not_in_result():
    calls = []

    def handler(request):
        calls.append(request)
        return httpx.Response(302, headers={"Location": "https://example.com/secret"})

    async def run():
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            secret="fixture-secret-7f39",
            transport=httpx.MockTransport(handler),
        )
        result = await adapter.generate({"stream": True}, 1)
        await adapter.close()
        return result

    result = asyncio.run(run())
    assert len(calls) == 1
    assert result["execution_state"] == "failed"
    assert "fixture-secret-7f39" not in json.dumps(result)


def test_health_is_not_idle_evidence():
    async def run():
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(
                lambda request: httpx.Response(200, json={"status": "ok"})
            ),
        )
        assert not await adapter.wait_idle(0.02)
        await adapter.close()

    asyncio.run(run())


def test_real_frozen_protocol_fixtures_parse():
    directory = Path(__file__).parents[1] / "fixtures/prism"
    decoder = SSEDecoder()
    state = ResponseState(0)
    protocol = StreamProtocol(state, Redactor(), lambda *args: None)
    for index, event in enumerate(decoder.feed((directory / "stream.sse").read_bytes()), 1):
        protocol.accept(event, index)
    assert state.done and state.finish_reason == "stop" and state.completion_tokens == 20
    assert json.loads((directory / "slots-idle.json").read_text())[0]["is_processing"] is False
    assert any(
        s["slots"][0]["is_processing"]
        for s in json.loads((directory / "slots-during.json").read_text())
    )
    template_tokens = json.loads((directory / "tokenize.json").read_text())["tokens"]
    assert (
        len(template_tokens)
        == json.loads((directory / "ordinary.json").read_text())["usage"]["prompt_tokens"]
        == 27
    )


@pytest.mark.parametrize("inputs,passes", [(3584, True), (3585, False)])
def test_context_budget_boundary(inputs, passes):
    def handler(request):
        return httpx.Response(
            200,
            json={"prompt": "rendered"}
            if request.url.path == "/apply-template"
            else {"tokens": [1] * inputs},
        )

    async def run():
        from inferyard.config.loader import load_config

        path = Path(__file__).parents[1] / "fixtures/config/valid.toml"
        config = load_config(path).config.to_dict()
        adapter = PrismAdapter("http://127.0.0.1:8080", transport=httpx.MockTransport(handler))
        try:
            if passes:
                assert (await adapter.token_budget(config, "hello"))["input_tokens"] == 3584
            else:
                with pytest.raises(PreflightError, match="context_budget_exceeded"):
                    await adapter.token_budget(config, "hello")
        finally:
            await adapter.close()

    asyncio.run(run())


def test_content_after_finish_is_a_failed_protocol_response():
    async def run():
        text = "".join(
            "data: " + event + "\n\n"
            for event in [frame({"content": "北京"}, "stop"), frame({"content": "extra"}), "[DONE]"]
        )
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(
                lambda request: httpx.Response(200, content=text.encode())
            ),
        )
        try:
            return await adapter.generate({"stream": True}, 1)
        finally:
            await adapter.close()

    result = asyncio.run(run())
    assert result["execution_state"] == "failed"
    assert result["error_category"] == "content_after_finish"
    assert result["content"] == "北京"


@pytest.mark.parametrize(
    "delta,reason", [({"content": ""}, "stop"), ({"reasoning_content": "仅思考"}, "length")]
)
def test_complete_without_final_answer_is_not_quality_success(delta, reason, wire_fixture):
    from inferyard.analysis.scoring import score_case

    async def run():
        payload = ("data: " + frame(delta, reason) + "\n\ndata: [DONE]\n\n").encode()
        adapter = PrismAdapter(
            "http://127.0.0.1:8080",
            transport=httpx.MockTransport(lambda request: httpx.Response(200, content=payload)),
        )
        try:
            return await adapter.generate({"stream": True}, 1)
        finally:
            await adapter.close()

    result = asyncio.run(run())
    bundle = wire_fixture("bundle")
    assert result["execution_state"] == "completed"
    assert result["content"] == ""
    assert (
        score_case(bundle["cases"][0], result["content"], bundle["answer_policy"])["quality_state"]
        == "fail"
    )
