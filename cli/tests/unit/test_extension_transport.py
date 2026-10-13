import asyncio
import hashlib
import json

import httpx
import pytest

from inferyard.extensions.extension_transport import ExtensionTransport
from inferyard.platforms.identity import PreflightError
from tests.unit.test_llama_cpp import inputs


def setup(config_path, *, slots=4):
    config = inputs(config_path)
    config["conditions"]["cache_policy"] = "disabled"
    config["model"]["template_sha256"] = hashlib.sha256(b"template").hexdigest()
    case = {
        "case_id": "case-a",
        "category": "extraction",
        "prompt": "get city",
        "reference_answer": '{"city":"北京"}',
        "rules": {
            "fields": {"city": {"type": "string", "value": "北京"}},
            "allow_extra_fields": False,
        },
    }
    params = {**config["generation"], "n_predict": config["generation"]["max_tokens"]}
    props = {
        "build_info": config["engine"]["release"],
        "total_slots": slots,
        "endpoint_slots": True,
        "default_generation_settings": {"n_ctx": config["conditions"]["context_size"]},
        "model_path": config["model"]["local_path"],
        "chat_template": "template",
        "chat_template_caps": {"supports_tool_calls": True},
    }
    rows = [{"id": i, "is_processing": False, "params": dict(params)} for i in range(slots)]
    requests = []

    def handle(request):
        requests.append(request)
        path = request.url.path
        if path == "/props":
            return httpx.Response(200, json=props)
        if path == "/slots":
            return httpx.Response(200, json=rows)
        if path == "/apply-template":
            return httpx.Response(200, json={"prompt": "rendered"})
        if path == "/tokenize":
            return httpx.Response(200, json={"tokens": list(range(12))})
        body = json.loads(request.content)
        if body.get("stream"):
            chunks = [
                {"choices": [{"index": 0, "delta": {"content": "10"}, "finish_reason": "stop"}]},
                {"choices": [], "usage": {"prompt_tokens": 12, "completion_tokens": 3}},
            ]
            data = "".join("data: " + json.dumps(c) + "\n\n" for c in chunks) + "data: [DONE]\n\n"
            return httpx.Response(200, content=data, headers={"content-type": "text/event-stream"})
        assert body["id_slot"] == 2 and body["cache_prompt"] is False
        return httpx.Response(
            200,
            json={
                "choices": [
                    {
                        "index": 0,
                        "finish_reason": "stop",
                        "message": {"role": "assistant", "content": '{"city":"北京"}'},
                    }
                ],
                "usage": {"prompt_tokens": 12, "completion_tokens": 3},
            },
        )

    adapter = ExtensionTransport(
        config, [case], concurrency=slots, transport=httpx.MockTransport(handle)
    )
    return adapter, props, rows, requests


def test_separate_multislot_transport_checks_template_tokens_effective_slot_and_quality(
    config_path,
):
    async def check():
        adapter, _, _, requests = setup(config_path)
        try:
            await adapter.verify()
            result = await adapter.infer("case-a", 2)
            assert result["quality_pass"] and result["protocol_complete"]
            assert await adapter.wait_idle(2, 0.1)
            assert sum(r.url.path == "/v1/chat/completions" for r in requests) == 1
        finally:
            await adapter.close()

    asyncio.run(check())


@pytest.mark.parametrize(
    "change,reason",
    [
        ("template", "template_mismatch"),
        ("slots", "slot_identity"),
        ("busy", "slots_busy"),
        ("tools", "native_tools_capability"),
        ("seed", "effective_parameter"),
    ],
)
def test_capability_and_parameter_gaps_fail_closed(config_path, change, reason):
    async def check():
        adapter, props, rows, _ = setup(config_path)
        try:
            if change == "template":
                props["chat_template"] = "different"
            if change == "slots":
                rows[1]["id"] = 0
            if change == "busy":
                rows[1]["is_processing"] = True
            if change == "tools":
                props["chat_template_caps"]["supports_tool_calls"] = False
            if change == "seed":
                rows[2]["params"]["seed"] = True
            with pytest.raises(PreflightError, match=reason):
                await adapter.verify(tools=change == "tools")
                if change == "seed":
                    await adapter.infer("case-a", 2)
        finally:
            await adapter.close()

    asyncio.run(check())


def test_native_stream_path_preserves_actual_usage_and_finish(config_path):
    async def check():
        adapter, _, _, _ = setup(config_path, slots=1)
        try:
            await adapter.verify(tools=True)
            reply = await adapter.completion(
                [{"role": "user", "content": "7+3"}], tools=[{"type": "function"}], stream=True
            )
            assert reply["usage"]["completion_tokens"] == 3
            assert reply["choices"][0]["message"]["content"] == "10"
        finally:
            await adapter.close()

    asyncio.run(check())
