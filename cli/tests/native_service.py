"""Unpatched OpenAI server with the observed release 404/health/models surface."""

import json

import httpx

from tests.lab_service import Stream
from tests.native_stream import usage_stream


class NativeService:
    def __init__(self, config):
        self.config = config
        self.calls, self.requests = [], []
        self.options = {}

    def __call__(self, request):
        self.calls.append(request)
        path = request.url.path
        if path == "/health" and not self.options.get("no_signals"):
            return httpx.Response(200, json={"status": "ok"})
        if path == "/v1/models" and not self.options.get("no_signals"):
            return httpx.Response(
                200, json={"data": [{"id": self.config["model"]["display_name"]}]}
            )
        if path == "/slots" and self.options.get("slots"):
            return httpx.Response(200, json=[])
        if path != "/v1/chat/completions":
            return httpx.Response(404)
        body = json.loads(request.content)
        self.requests.append(body)
        fault = self.options.get("fail_at", len(self.requests)) == len(self.requests)
        if fault and self.options.get("http_error"):
            return httpx.Response(500)
        assert "lab_request_id" not in body and "cache_policy" not in body
        assert "reasoning_mode" not in body and "X-Lab-Request-ID" not in request.headers
        usage = {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
        if not body["stream"]:
            return httpx.Response(
                200,
                json={
                    "id": "chatcmpl-server-id",
                    "choices": [
                        {
                            "index": 0,
                            "message": {"role": "assistant", "content": "北京"},
                            "finish_reason": "stop",
                        }
                    ],
                    "usage": usage,
                },
            )

        if fault and "usage_mode" in self.options:
            return httpx.Response(200, stream=Stream(usage_stream(self.options["usage_mode"])))

        def frame(value):
            return ("data: " + json.dumps(value, ensure_ascii=False) + "\n\n").encode()

        chunks = [
            frame(
                {
                    "id": "chatcmpl-server-id",
                    "choices": [{"index": 0, "delta": {"content": "北京"}, "finish_reason": None}],
                }
            ),
            frame(
                {
                    "id": "chatcmpl-server-id",
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
                }
            ),
            frame({"id": "chatcmpl-server-id", "choices": [], "usage": usage}),
        ]
        if not (fault and self.options.get("truncated")):
            chunks.append(b"data: [DONE]\n\n")
        return httpx.Response(
            200, stream=Stream(chunks, delay=self.options.get("delay", 0) if fault else 0)
        )
