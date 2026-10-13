"""Synthetic lab endpoint for mock transport tests; no model or native engine involved."""

import asyncio
import hashlib
import json
from copy import deepcopy

import httpx

from inferyard.adapters.lab_observation_json import ACTIVE_PHASES, PROTOCOL
from inferyard.adapters.lab_parameters import CONDITIONS

INSTANCE = "1" * 32


def lab_config(base, engine):
    config = deepcopy(base)
    model, settings = config["model"], config["engine"]
    kind = "gguf" if engine == "kvmem" else "ninfer"
    model.update(
        kind=kind,
        bytes=100,
        local_path=rf"D:\lab\model.{kind}",
        template_path=r"D:\lab\template.jinja",
    )
    settings.update(
        adapter=engine,
        release="fixture-lab-build",
        binary_path=r"D:\lab\engine.exe",
        working_directory=r"D:\lab",
        startup_args=["--model", model["local_path"]],
        observation_mode="lab_required",
    )
    entries = [
        {"path": model["local_path"], "role": "model", "bytes": 100, "sha256": model["sha256"]},
        {
            "path": settings["binary_path"],
            "role": "engine",
            "bytes": 10,
            "sha256": settings["binary_sha256"],
        },
        {
            "path": model["template_path"],
            "role": "template",
            "bytes": 5,
            "sha256": model["template_sha256"],
        },
    ]
    if engine == "ninfer":
        model.update(
            component_ledger_path=r"D:\lab\components.json", component_ledger_sha256="c" * 64
        )
        entries.append(
            {
                "path": model["component_ledger_path"],
                "role": "component_ledger",
                "bytes": 100,
                "sha256": model["component_ledger_sha256"],
            }
        )
    settings["asset_manifest"] = entries
    config["generation"].update(seed_support="supported", reasoning_mode="off")
    config["conditions"]["cache_policy"] = "disabled"
    config["endpoint"].pop("api_key_env", None)
    return config


class Stream(httpx.AsyncByteStream):
    def __init__(self, chunks, delay=0):
        self.chunks, self.delay = chunks, delay

    async def __aiter__(self):
        for chunk in self.chunks:
            await asyncio.sleep(self.delay)
            yield chunk


class LabService:
    def __init__(self, config):
        self.config = config
        self.calls = []
        self.seq = 0
        self.requests = {}
        self.options = {}
        self.identity_value = {
            "protocol": PROTOCOL,
            "server_instance_id": INSTANCE,
            "engine": config["engine"]["adapter"],
            "build_id": config["engine"]["release"],
            "model": {key: config["model"][key] for key in ("kind", "bytes", "sha256")},
            "template_sha256": config["model"]["template_sha256"],
            "capabilities": {
                key: True
                for key in (
                    "lifecycle",
                    "request_cancel",
                    "request_id",
                    "token_budget",
                    "effective_parameters",
                )
            },
        }

    def response(self, payload):
        return httpx.Response(200, json=payload)

    def __call__(self, request):
        self.calls.append(request)
        path = request.url.path
        instance = self.identity_value["server_instance_id"]
        if path in self.options.get("missing", []):
            return httpx.Response(404, json={"error": "unsupported"})
        if path in ("/health", "/v1/models", "/slots", "/metrics", "/props"):
            return httpx.Response(404)
        if path == "/lab/v1/identity":
            return self.response(self.identity_value)
        assert request.headers["X-Lab-Instance-ID"] == instance
        if path == "/lab/v1/lifecycle":
            self.seq += 1
            busy = self.options.get("busy", False)
            active = [
                {"request_id": key, "phase": "releasing", "cancel_requested": value["cancel"]}
                for key, value in self.requests.items()
                if busy
            ]
            stages = dict.fromkeys(ACTIVE_PHASES, 0)
            stages["releasing"] = len(active)
            return self.response(
                {
                    "protocol": PROTOCOL,
                    "server_instance_id": instance,
                    "snapshot_seq": self.seq,
                    "active_total": len(active),
                    "poisoned": self.options.get("poisoned", False),
                    "stages": stages,
                    "requests": active,
                }
            )
        if path == "/lab/v1/token-budget":
            return self.response(
                {
                    "protocol": PROTOCOL,
                    "server_instance_id": instance,
                    "input_tokens": self.options.get("input_tokens", 1),
                    "context_size": self.config["conditions"]["context_size"],
                    "template_sha256": self.config["model"]["template_sha256"],
                    "template_prompt_sha256": hashlib.sha256(b"rendered").hexdigest(),
                }
            )
        if path == "/v1/chat/completions":
            body = json.loads(request.content)
            key = request.headers["X-Lab-Request-ID"]
            assert body["lab_request_id"] == key
            self.requests[key] = {"body": body, "cancel": False}
            prompt = body["messages"][0]["content"]
            answer = '{"name":"小明","age":12}' if "姓名" in prompt else "北京"
            answer = self.options.get("answer", answer)
            value = {
                "lab_request_id": key,
                "lab_server_instance_id": instance,
                "choices": [
                    {
                        "index": 0,
                        "finish_reason": "stop",
                        "delta" if body["stream"] else "message": {"content": answer},
                    }
                ],
                "usage": {"prompt_tokens": 1, "completion_tokens": 2},
            }
            if self.options.get("mismatched_id"):
                value["lab_request_id"] = "f" * 32
            if self.options.get("http_error"):
                return httpx.Response(500, json={"error": "fixture"})
            if body["stream"]:
                raw = ("data: " + json.dumps(value, ensure_ascii=False) + "\n\n").encode()
                if not self.options.get("truncated"):
                    raw += b"data: [DONE]\n\n"
                if self.options.get("after_done"):
                    raw += b"data: {}\n\n"
                return httpx.Response(200, stream=Stream([raw], self.options.get("delay", 0)))
            return self.response(value)
        key = path.split("/")[4]
        row = self.requests[key]
        if path.endswith("/cancel"):
            if self.options.get("cancel_error"):
                return httpx.Response(503)
            row["cancel"] = True
            return self.response(
                {
                    "protocol": PROTOCOL,
                    "server_instance_id": instance,
                    "request_id": key,
                    "disposition": "accepted",
                }
            )
        if path.endswith("/parameters"):
            value = {
                "protocol": PROTOCOL,
                "server_instance_id": instance,
                "request_id": key,
                "generation": deepcopy(self.config["generation"]),
                "conditions": {k: self.config["conditions"][k] for k in CONDITIONS},
                "auxiliary_routes": {"vision": False, "speculative": False, "ngram": False},
            }
            if self.options.get("effective_mismatch"):
                value["generation"]["seed"] += 1
            return self.response(value)
        self.seq += 1
        busy = self.options.get("busy", False)
        return self.response(
            {
                "protocol": PROTOCOL,
                "server_instance_id": instance,
                "snapshot_seq": self.seq,
                "request_id": key,
                "phase": "releasing" if busy else "released",
                "cancel_requested": row["cancel"],
                "outcome": None if busy else "cancelled" if row["cancel"] else "completed",
            }
        )
