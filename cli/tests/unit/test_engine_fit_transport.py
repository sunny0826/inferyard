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


@pytest.mark.parametrize("engine,path", [("vllm", "/version"), ("sglang", "/get_server_info")])
def test_inspect_exact_model_and_version_only(engine, path):
    seen = []

    def handler(request):
        seen.append(request.url.path)
        if request.url.path == "/v1/models":
            return httpx.Response(200, json={"data": [{"id": "fixture-model"}]})
        assert request.url.path == path
        return httpx.Response(200, json={"version": "0.5.2", "api_key": "do-not-persist"})

    result = run_request(handler, "inspect", engine=engine)
    assert result == {
        "engine": engine,
        "version": "0.5.2",
        "served_model": "fixture-model",
        "version_source": path,
    }
    assert seen == ["/v1/models", path]
    assert "do-not-persist" not in json.dumps(result)


@pytest.mark.parametrize(
    "models",
    [
        None,
        {},
        [True],
        [{"id": 1}],
        [{"id": "other"}],
        [{"id": "fixture-model"}, {"id": "fixture-model"}],
    ],
)
def test_inspect_rejects_missing_malformed_or_duplicate_models(models):
    with pytest.raises(FitTransportError):
        run_request(lambda _: httpx.Response(200, json={"data": models}), "inspect")


@pytest.mark.parametrize("ids", [["fixture-model", "lora-model"], ["alias", "fixture-model"]])
def test_inspect_rejects_multiple_models_or_aliases_before_version_probe(ids):
    seen = []

    def handler(request):
        seen.append(request.url.path)
        return httpx.Response(200, json={"data": [{"id": model_id} for model_id in ids]})

    with pytest.raises(FitTransportError, match="^served_model_unverified$"):
        run_request(handler, "inspect")
    assert seen == ["/v1/models"]


@pytest.mark.parametrize("version", [None, True, 1, "", "service error contains a secret"])
def test_inspect_rejects_unverified_version(version):
    def handler(request):
        payload = (
            {"data": [{"id": "fixture-model"}]}
            if request.url.path == "/v1/models"
            else {"version": version}
        )
        return httpx.Response(200, json=payload)

    with pytest.raises(FitTransportError, match="^engine_version_unverified$"):
        run_request(handler, "inspect")


@pytest.mark.parametrize("engine", ["vllm", "sglang"])
def test_stream_final_empty_choices_usage_and_split_unicode(engine):
    events = [
        frame(),
        frame("北"),
        frame("京", "stop"),
        {"choices": [], "usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}},
        "[DONE]",
    ]
    raw = b": keepalive\r\n\r\n" + sse(*events).replace(b"\n", b"\r\n")
    stream = Chunks([raw[i : i + 1] for i in range(len(raw))])

    def handler(request):
        assert request.url.path == "/v1/chat/completions"
        assert request.headers["authorization"] == "Bearer fixture-secret"
        assert json.loads(request.content) == {
            "model": "fixture-model",
            "messages": [{"role": "user", "content": "fixture prompt"}],
            "max_tokens": 16,
            "temperature": 0,
            "stream": True,
            "stream_options": {"include_usage": True},
        }
        return stream_response(stream=stream)

    result = run_request(handler, engine=engine, api_key="fixture-secret")
    assert result["text"] == "北京"
    assert result["finish_reason"] == "stop"
    assert result["prompt_tokens"] == 5 and result["completion_tokens"] == 3
    assert result["usage_missing_reason"] is None
    assert 0 <= result["first_content_ms"] <= result["elapsed_ms"]
    assert stream.closed


@pytest.mark.parametrize("engine", ["llama-cpp", "vllm", "sglang"])
@pytest.mark.parametrize(
    "details",
    [
        {
            "prompt_tokens_details": {"cached_tokens": 0},
            "completion_tokens_details": {"reasoning_tokens": 0},
        },
        {"prompt_tokens_details": None, "completion_tokens_details": None},
    ],
)
def test_nested_usage_details_are_not_rejected_as_token_counters(engine, details):
    usage = {"prompt_tokens": 28, "completion_tokens": 3, "total_tokens": 31, **details}
    raw = sse(frame("42", "stop"), {"choices": [], "usage": usage}, "[DONE]")
    stream = Chunks([raw[i : i + 1] for i in range(len(raw))])

    result = run_request(lambda _: stream_response(stream=stream), engine=engine)

    assert result["text"] == "42"
    assert result["finish_reason"] == "stop"
    assert result["prompt_tokens"] == 28
    assert result["completion_tokens"] == 3
    assert result["usage_missing_reason"] is None
    assert stream.closed


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
    "events,reason",
    [
        ([frame("text", "stop")], "missing_done"),
        ([frame("text"), "[DONE]"], "missing_finish_reason"),
        ([frame("text", "tool_calls"), "[DONE]"], "unsupported_finish_reason"),
        ([frame("text", "stop"), "[DONE]", frame("extra")], "event_after_done"),
        ([frame("text", "stop"), frame("extra"), "[DONE]"], "choice_after_finish"),
        ([frame(index=True)], "invalid_choice_index"),
        ([frame(index=1)], "invalid_choice_index"),
        ([frame(2)], "invalid_content"),
        ([{"choices": []}], "empty_stream_event"),
        ([{"choices": [{"index": 0, "delta": {"tool_calls": [{}]}}]}], "unsupported_tool_response"),
        ([{"choices": [frame()["choices"][0], frame()["choices"][0]]}], "invalid_choices"),
        ([{"choices": [], "error": {"message": "secret-output"}}], "service_error"),
        (['{"choices": [], "choices": []}'], "invalid_json"),
        (['{"choices": [], "usage": NaN}'], "invalid_json"),
        (['{"choices": [], "usage": 1e999}'], "invalid_json"),
    ],
)
def test_stream_protocol_failures_are_fixed_reasons(events, reason):
    with pytest.raises(FitTransportError) as error:
        run_request(lambda _: stream_response(events))
    assert error.value.reason == reason
    assert str(error.value) == reason


@pytest.mark.parametrize(
    "usage",
    [
        True,
        [],
        {"completion_tokens": True},
        {"prompt_tokens": -1},
        {"prompt_tokens": 1.0},
        {"completion_tokens": "1"},
        {"prompt_tokens": 2, "completion_tokens": 3, "total_tokens": 9},
        {"completion_tokens": True, "prompt_tokens_details": {"cached_tokens": 0}},
    ],
)
def test_invalid_usage_is_not_silently_discarded(usage):
    with pytest.raises(FitTransportError, match="^(invalid|inconsistent)_usage$"):
        run_request(
            lambda _: stream_response(
                [frame("answer", "stop"), {"choices": [], "usage": usage}, "[DONE]"]
            )
        )


@pytest.mark.parametrize(
    "raw,reason",
    [
        (b"data: \xff\n\n", "invalid_utf8"),
        (b"data: [DONE]", "incomplete_sse_event"),
        (b"data: {}\n", "incomplete_sse_event"),
        (b"garbage\n\n", "invalid_sse_field"),
    ],
)
def test_malformed_sse_is_rejected(raw, reason):
    with pytest.raises(FitTransportError, match=f"^{reason}$"):
        run_request(lambda _: stream_response(stream=Chunks([raw])))


def test_total_deadline_expires_despite_continuous_heartbeats():
    stream = Chunks([b": heartbeat\n\n"] * 100, delay=0.002)
    with pytest.raises(FitTransportError, match="^request_timeout$"):
        run_request(lambda _: stream_response(stream=stream), timeout=0.01)
    assert stream.closed


def test_external_cancellation_propagates_and_closes_stream():
    stream = Chunks([b": heartbeat\n\n"] * 100, delay=0.002)

    async def run():
        async with FitClient(
            "vllm",
            "http://127.0.0.1:8000",
            "fixture-model",
            transport=httpx.MockTransport(lambda _: stream_response(stream=stream)),
        ) as client:
            task = asyncio.create_task(client.complete("prompt", 8, 10))
            await asyncio.sleep(0.005)
            task.cancel()
            with pytest.raises(asyncio.CancelledError):
                await task

    asyncio.run(run())
    assert stream.closed


@pytest.mark.parametrize("method", ["complete", "inspect", "idle"])
def test_every_response_is_size_bounded(monkeypatch, method):
    monkeypatch.setattr(engine_fit, "MAX_RESPONSE_BYTES", 20)
    with pytest.raises(FitTransportError, match="^response_too_large$"):
        run_request(lambda _: stream_response(stream=Chunks([b" " * 12] * 2)), method)


@pytest.mark.parametrize("status", [301, 307, 401, 500])
def test_http_error_body_and_redirect_target_never_escape(status):
    seen = []

    def handler(request):
        seen.append(request.url)
        return httpx.Response(
            status,
            headers={"location": "https://remote.example/secret"},
            content=b"sensitive error details",
        )

    with pytest.raises(FitTransportError, match="^http_status_error$"):
        run_request(handler)
    assert len(seen) == 1


def test_network_errors_are_sanitized():
    def handler(_):
        raise httpx.ReadError("raw-secret-in-network-error")

    with pytest.raises(FitTransportError, match="^http_transport_error$") as error:
        run_request(handler)
    assert error.value.__suppress_context__


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


def test_proxy_environment_is_ignored(monkeypatch):
    for name in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY"):
        monkeypatch.setenv(name, "invalid-proxy-url")

    async def run():
        async with FitClient("vllm", "http://127.0.0.1:8000", "fixture-model") as client:
            assert client._client.trust_env is False
            assert client._client.follow_redirects is False

    asyncio.run(run())


@pytest.mark.parametrize(
    "engine,names",
    [
        ("vllm", ("vllm:num_requests_running", "vllm:num_requests_waiting")),
        ("sglang", ("sglang:num_running_reqs", "sglang:num_queue_reqs")),
    ],
)
def test_all_metric_series_participate_in_idle(engine, names):
    text = "\n".join(
        f'{name}{{model_name="fixture-model",rank="{rank}"}} 0' for name in names for rank in (0, 1)
    )

    def handler(_):
        return httpx.Response(200, text=text)

    result = run_request(handler, "idle", engine=engine)
    assert result["idle"] is True and len(result["values"]) == 4
    text = text.replace('rank="1"} 0', 'rank="1"} 1', 1)
    assert run_request(handler, "idle", engine=engine)["idle"] is False


@pytest.mark.parametrize("optional", ["vllm:num_requests_swapped", "sglang:num_grammar_queue_reqs"])
def test_optional_active_queue_prevents_idle(optional):
    engine = optional.split(":")[0]
    text = "\n".join(f"{name} 0" for name in engine_fit._METRICS[engine]) + f"\n{optional} 1"
    assert (
        run_request(lambda _: httpx.Response(200, text=text), "idle", engine=engine)["idle"]
        is False
    )


@pytest.mark.parametrize(
    "extra,reason",
    [
        ("vllm:num_requests_running 0", "duplicate_metrics_series"),
        ("vllm:num_requests_swapped NaN", "invalid_metrics_value"),
        ("vllm:num_requests_swapped +Inf", "invalid_metrics_value"),
        ("vllm:num_requests_swapped -1", "invalid_metrics_value"),
        ("vllm:num_requests_swapped 0.5", "invalid_metrics_value"),
        ("vllm:num_requests_swapped true", "invalid_metrics_value"),
        ('vllm:num_requests_swapped{x="a",x="b"} 0', "invalid_metrics_labels"),
        ('vllm:num_requests_swapped{x="a",bad} 0', "invalid_metrics_labels"),
        ('vllm:num_requests_swapped{x="a\\q"} 0', "invalid_metrics_labels"),
        ("vllm:num_requests_swapped{} garbage trailing text", "invalid_metrics_sample"),
    ],
)
def test_invalid_metrics_fail_closed(extra, reason):
    text = f"vllm:num_requests_running 0\nvllm:num_requests_waiting 0\n{extra}"
    with pytest.raises(FitTransportError, match=f"^{reason}$"):
        run_request(lambda _: httpx.Response(200, text=text), "idle")


def test_reordered_labels_do_not_hide_duplicate_metrics():
    text = (
        'vllm:num_requests_running{a="1",b="2"} 0\n'
        'vllm:num_requests_running{b="2",a="1"} 0\nvllm:num_requests_waiting 0'
    )
    with pytest.raises(FitTransportError, match="^duplicate_metrics_series$"):
        run_request(lambda _: httpx.Response(200, text=text), "idle")


@pytest.mark.parametrize(
    "text,reason",
    [
        ("unrelated 0", "missing_idle_metrics"),
        ("vllm:num_requests_running 0", "missing_idle_metrics"),
        (
            'vllm:num_requests_running{rank="0"} 0\nvllm:num_requests_waiting{rank="1"} 0',
            "incomplete_idle_metrics_series",
        ),
    ],
)
def test_missing_or_partial_metrics_rejected(text, reason):
    with pytest.raises(FitTransportError, match=f"^{reason}$"):
        run_request(lambda _: httpx.Response(200, text=text), "idle")


@pytest.mark.parametrize(
    "field,value",
    [
        ("max_tokens", True),
        ("max_tokens", -1),
        ("max_tokens", 1.5),
        ("timeout_seconds", True),
        ("timeout_seconds", float("nan")),
        ("timeout_seconds", float("inf")),
        ("timeout_seconds", 0),
        ("prompt", ""),
    ],
)
def test_invalid_request_never_sent(field, value):
    async def run():
        def handler(_):
            pytest.fail("Invalid request was sent")

        async with FitClient(
            "vllm", "http://127.0.0.1:8000", "fixture-model", transport=httpx.MockTransport(handler)
        ) as client:
            args = {"prompt": "test", "max_tokens": 8, "timeout_seconds": 1, field: value}
            with pytest.raises(FitTransportError):
                await client.complete(**args)

    asyncio.run(run())


@pytest.mark.parametrize("method", ["inspect", "idle"])
def test_inspection_deadline_applies_to_streamed_bodies(monkeypatch, method):
    monkeypatch.setattr(engine_fit, "INSPECTION_TIMEOUT_SECONDS", 0.005)
    stream = Chunks([b" "] * 100, delay=0.002)
    with pytest.raises(FitTransportError, match="^request_timeout$"):
        run_request(lambda _: httpx.Response(200, stream=stream), method)
    assert stream.closed


def test_escaped_surrogates_are_rejected_before_evidence_serialization():
    with pytest.raises(FitTransportError, match="^invalid_json$"):
        run_request(
            lambda _: stream_response(
                [
                    '{"choices":[{"index":0,"delta":{"content":"\\ud800"},"finish_reason":"stop"}]}',
                    "[DONE]",
                ]
            )
        )


@pytest.mark.parametrize(
    "headers,reason",
    [
        ({"content-type": "application/json"}, "invalid_sse_content_type"),
        ({"content-encoding": "br"}, "unsupported_content_encoding"),
    ],
)
def test_unsupported_response_format_rejected(headers, reason):
    with pytest.raises(FitTransportError, match=f"^{reason}$"):
        run_request(lambda _: httpx.Response(200, headers=headers, stream=Chunks([b""])))


@pytest.mark.parametrize("api_key", ["", "secret\nvalue", "secret\x00value", "凭据"])
def test_invalid_auth_header_never_reaches_httpx(api_key):
    with pytest.raises(FitTransportError, match="^invalid_api_key$"):
        FitClient("vllm", "http://127.0.0.1", "fixture-model", api_key)
