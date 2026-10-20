import json
from copy import deepcopy
from types import SimpleNamespace

import httpx
import pytest

from inferyard.adapters.local_engines import LocalEngineAdapter
from inferyard.adapters.local_engines.protocol import mlx_sources

METRICS = {
    "llama-cpp": "llamacpp:requests_processing 0\nllamacpp:requests_deferred 0\n",
    "mlx-lm": (
        "engine_fit:num_requests_running 0\n"
        "engine_fit:num_requests_waiting 0\nengine_fit:poisoned 0\n"
    ),
    "vllm": "vllm:num_requests_running 0\nvllm:num_requests_waiting 0\n",
    "sglang": "sglang:num_running_reqs 0\nsglang:num_queue_reqs 0\n",
    "lmstudio": "",
}


class Observer:
    def __init__(self):
        self.active = self.queued = 0

    async def inspect(self):
        return {
            "engine": "lmstudio",
            "version": None,
            "served_model": "fixture",
            "version_source": "not_exposed",
            "version_missing_reason": "lmstudio_service_version_not_exposed",
        }

    async def idle(self):
        return {
            "idle": self.active == self.queued == 0,
            "source": "lms:ps",
            "values": [
                {
                    "metric": "lmstudio:queued",
                    "labels": {"instance": "fixture"},
                    "value": self.queued,
                },
                {
                    "metric": "lmstudio:active",
                    "labels": {"instance": "fixture"},
                    "value": self.active,
                },
            ],
        }


@pytest.fixture
def rig():
    def create(family="vllm", *, completion=None):
        config = {
            "engine": {
                "adapter": "local_openai_v1",
                "family": family,
                "served_model": "fixture",
                "installation_sha256": "a" * 64,
            },
            "generation": {"temperature": 0, "max_tokens": 32},
            "conditions": {"context_size": 4096},
            "execution": {"idle_wait_seconds": 0.1},
            "model": {"local_path": "/fixture/model"},
            "endpoint": {
                "url": "http://127.0.0.1:8765",
                "server_pid": 123,
                "process_start_ticks": 456,
            },
        }
        binding = {
            "pid": 123,
            "start_ticks": 456,
            "origin": "http://127.0.0.1:8765",
            "address": "127.0.0.1",
            "port": 8765,
            "executable_sha256": "b" * 64,
            "argv_sha256": "c" * 64,
            "cwd_sha256": "d" * 64,
            "listener_inode": None,
            "listener_identity": "listener",
            "listener_source": "lsof:TCP:LISTEN:pid",
            "model_binding": {
                "engine": family,
                "path": "/fixture/model",
                "device": 1,
                "inode": 2,
                "source": "verified_startup_directory_identity",
            },
        }
        observer = Observer()
        if family == "lmstudio":
            binding["observer"] = {
                "kind": "lms",
                "path": "/fixture/lms",
                "sha256": "e" * 64,
                "models_root": "/fixture",
                "instance_id": "fixture",
            }
        state = SimpleNamespace(
            config=config,
            binding=binding,
            metrics=METRICS[family],
            posts=[],
            observer=observer,
            version={
                "version": "1.2.3",
                "build_info": "b123-abcd123",
                "local_engine_protocol": "mlx-lm-formal.v1",
                "synthetic": False,
                "max_model_len": 4096,
                **mlx_sources(),
            },
        )

        async def handler(request):
            if request.method == "POST":
                body = json.loads(request.content)
                state.posts.append(body)
                if completion:
                    return await completion(request)
                choice = {"index": 0, "finish_reason": "stop"}
                usage = {"prompt_tokens": 2, "completion_tokens": 1, "total_tokens": 3}
                if body["stream"]:
                    chunks = [
                        {
                            "choices": [
                                {"index": 0, "delta": {"content": "ok"}, "finish_reason": None}
                            ]
                        },
                        {"choices": [{**choice, "delta": {}}]},
                        {"choices": [], "usage": usage},
                    ]
                    raw = (
                        "".join("data: " + json.dumps(c) + "\n\n" for c in chunks)
                        + "data: [DONE]\n\n"
                    )
                    return httpx.Response(
                        200, text=raw, headers={"content-type": "text/event-stream"}
                    )
                return httpx.Response(
                    200,
                    json={"choices": [{**choice, "message": {"content": "ok"}}], "usage": usage},
                )
            path = request.url.path
            if path == "/v1/models":
                return httpx.Response(200, json={"data": [{"id": "fixture"}]})
            if path == "/metrics":
                return httpx.Response(200, text=state.metrics)
            return httpx.Response(200, json=state.version)

        state.adapter = LocalEngineAdapter(
            binding["origin"],
            transport=httpx.MockTransport(handler),
            binding_verifier=lambda _: deepcopy(state.binding),
            observer=observer if family == "lmstudio" else None,
        )
        return state

    return create
