"""Unit tests for PolyStrata HTTP protocol adapter and service discovery."""

from __future__ import annotations

import asyncio
import json
from copy import deepcopy

import httpx
import jsonschema
import pytest

from inferyard import cli
from inferyard.adapters.local_engines.adapter import LocalEngineAdapter
from inferyard.application import service_discovery as discovery_app
from inferyard.contracts.schemas import export_schema
from inferyard.errors import PreflightError
from local_engine_helpers import local_config  # noqa: F401


def _polystrata_config(local_config, *, context_size: int = 32768, effort: str | None = "none"):
    cfg = deepcopy(local_config)
    cfg["model"]["kind"] = "gguf"
    cfg["model"]["local_path"] = "/models/qwen36.gguf"
    cfg["engine"].update(
        family="polystrata",
        backend="metal",
        working_directory="/opt/polystrata",
        polystrata_config_path="/out/strata-candidate.json",
        polystrata_config_sha256="a" * 64,
        served_model="qwen36-local",
        startup_args=[
            "/opt/polystrata/serve/server.py",
            "--engine",
            "strata",
            "--config",
            "/out/strata-candidate.json",
            "--host",
            "127.0.0.1",
            "--port",
            "48857",
        ],
        environment={"PYTHONDONTWRITEBYTECODE": "1"},
    )
    cfg["conditions"]["context_size"] = context_size
    if effort is not None:
        cfg["generation"]["reasoning_effort"] = effort
    else:
        cfg["generation"].pop("reasoning_effort", None)
    return cfg


def _prom_text(*, processing: int = 0, deferred: int = 0, loaded: int = 1) -> str:
    return (
        "# HELP llamacpp:requests_processing Requests running\n"
        "# TYPE llamacpp:requests_processing gauge\n"
        f"llamacpp:requests_processing {processing}\n"
        "# HELP llamacpp:requests_deferred Requests waiting their turn\n"
        "# TYPE llamacpp:requests_deferred gauge\n"
        f"llamacpp:requests_deferred {deferred}\n"
        "# HELP strata:model_loaded 1 while the model is loaded\n"
        "# TYPE strata:model_loaded gauge\n"
        f"strata:model_loaded {loaded}\n"
    )


def _fake_binding(config):
    return {"pid": 500, "origin": config["endpoint"]["url"]}


def test_polystrata_discover_wait_ready_prometheus_and_streaming_generate(local_config) -> None:
    cfg = _polystrata_config(local_config, context_size=32768, effort="none")
    prom_calls = 0
    posted_bodies: list[dict] = []

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal prom_calls
        path = request.url.path
        if path == "/v1/models":
            return httpx.Response(
                200,
                json={"object": "list", "data": [{"id": "qwen36-local", "object": "model"}]},
            )
        if path == "/health":
            return httpx.Response(
                200,
                json={
                    "status": "ok",
                    "service": "strata",
                    "loaded": True,
                    "model": "qwen36-local",
                    "max_context": 32768,
                },
            )
        if path == "/v1/status":
            return httpx.Response(
                200,
                json={
                    "service": "strata",
                    "model": "qwen36-local",
                    "loaded": True,
                    "engine": "v0.1.45-poly.10",
                    "cache_max_tokens": 32768,
                    "context": {"native": 32768, "max_positions": 32768},
                },
            )
        if path == "/metrics/prometheus":
            prom_calls += 1
            # First wait_ready poll (call #2 after discover) is busy, then idle
            busy = 1 if prom_calls == 2 else 0
            return httpx.Response(
                200,
                text=_prom_text(processing=busy, deferred=0, loaded=1),
                headers={"content-type": "text/plain; version=0.0.4; charset=utf-8"},
            )
        if path == "/v1/chat/completions":
            posted_bodies.append(json.loads(request.content.decode("utf-8")))
            sse = (
                'data: {"id":"cmpl-1","object":"chat.completion.chunk","model":"qwen36-local",'
                '"choices":[{"index":0,"delta":{"role":"assistant","content":"Beijing"},'
                '"finish_reason":null}]}\n\n'
                'data: {"id":"cmpl-1","object":"chat.completion.chunk","model":"qwen36-local",'
                '"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],'
                '"usage":{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}}\n\n'
                "data: [DONE]\n\n"
            )
            return httpx.Response(
                200,
                content=sse.encode("utf-8"),
                headers={"content-type": "text/event-stream"},
            )
        return httpx.Response(404)

    async def scenario():
        adapter = LocalEngineAdapter(
            "http://127.0.0.1:48857",
            transport=httpx.MockTransport(handler),
            binding_verifier=_fake_binding,
        )
        props = await adapter.verify_properties(cfg)
        assert props["service"] == {
            "engine": "polystrata",
            "version": "v0.1.45-poly.10",
            "served_model": "qwen36-local",
            "max_context": 32768,
            "version_source": "/v1/status",
        }
        snapshots: list[dict] = []
        ready = await adapter.wait_ready(
            seconds=2.0, observe=lambda _state, ev: snapshots.append(ev)
        )
        assert ready is True
        assert len(snapshots) == 2
        assert snapshots[0]["signals"]["queue_snapshot"]["source"] == "/metrics/prometheus"
        assert snapshots[0]["signals"]["queue_snapshot"]["observation"]["idle"] is False
        assert snapshots[1]["signals"]["queue_snapshot"]["observation"]["idle"] is True

        body = adapter.request_body(cfg, "Capital of China?", stream=True)
        assert body["reasoning_effort"] == "none"
        result = await adapter.generate(body, timeout=5.0)
        assert result["execution_state"] == "completed"
        assert result["content"] == "Beijing"
        assert posted_bodies[0]["reasoning_effort"] == "none"
        await adapter.close()

    asyncio.run(scenario())


def test_polystrata_discover_rejects_unloaded_and_mismatches(local_config) -> None:
    cfg = _polystrata_config(local_config, context_size=32768)

    def make_handler(health_payload, status_payload):
        def handler(request: httpx.Request) -> httpx.Response:
            if request.url.path == "/v1/models":
                return httpx.Response(200, json={"data": [{"id": "qwen36-local"}]})
            if request.url.path == "/health":
                return httpx.Response(200, json=health_payload)
            if request.url.path == "/v1/status":
                return httpx.Response(200, json=status_payload)
            if request.url.path == "/metrics/prometheus":
                return httpx.Response(200, text=_prom_text())
            return httpx.Response(404)

        return handler

    base_health = {
        "service": "strata",
        "loaded": True,
        "model": "qwen36-local",
        "max_context": 32768,
    }
    base_status = {
        "service": "strata",
        "loaded": True,
        "model": "qwen36-local",
        "engine": "v0.1.45-poly.10",
        "max_context": 32768,
    }

    cases = [
        ({**base_health, "loaded": False}, base_status, "polystrata_model_not_loaded"),
        ({**base_health, "service": "other"}, base_status, "polystrata_service_unverified"),
        (base_health, {**base_status, "max_context": 16384}, "polystrata_context_mismatch"),
        (base_health, {**base_status, "engine": None}, "engine_version_unverified"),
    ]
    async def run_one(health_p, status_p, expected_err):
        adapter = LocalEngineAdapter(
            "http://127.0.0.1:48857",
            transport=httpx.MockTransport(make_handler(health_p, status_p)),
            binding_verifier=_fake_binding,
        )
        try:
            with pytest.raises(PreflightError, match=expected_err):
                await adapter.discover(cfg)
        finally:
            await adapter.close()

    for health_p, status_p, expected_err in cases:
        asyncio.run(run_one(health_p, status_p, expected_err))


def test_polystrata_wait_ready_status_fallback_and_unloaded_metric(local_config) -> None:
    cfg = _polystrata_config(local_config, context_size=32768)
    status_calls = 0

    def status_only_handler(request: httpx.Request) -> httpx.Response:
        nonlocal status_calls
        path = request.url.path
        if path == "/v1/models":
            return httpx.Response(200, json={"data": [{"id": "qwen36-local"}]})
        if path == "/health":
            return httpx.Response(
                200,
                json={"service": "strata", "loaded": True, "model": "qwen36-local", "max_context": 32768},
            )
        if path == "/v1/status":
            return httpx.Response(
                200,
                json={
                    "service": "strata",
                    "loaded": True,
                    "model": "qwen36-local",
                    "engine": "v0.1.45-poly.10",
                    "max_context": 32768,
                },
            )
        if path == "/metrics/prometheus":
            return httpx.Response(404)
        if path == "/status":
            status_calls += 1
            if status_calls == 2:
                return httpx.Response(200, json={"busy": True, "queued": 1})
            return httpx.Response(200, json={"busy": False, "queued": 0})
        return httpx.Response(404)

    async def run_fallback():
        adapter = LocalEngineAdapter(
            "http://127.0.0.1:48857",
            transport=httpx.MockTransport(status_only_handler),
            binding_verifier=_fake_binding,
        )
        await adapter.discover(cfg)
        events: list[dict] = []
        ready = await adapter.wait_ready(seconds=2.0, observe=lambda _s, ev: events.append(ev))
        assert ready is True
        assert len(events) == 2
        assert events[0]["signals"]["queue_snapshot"]["source"] == "/status"
        assert events[0]["signals"]["queue_snapshot"]["observation"] == {
            "idle": False,
            "busy": True,
            "queued": 1,
        }
        assert events[1]["signals"]["queue_snapshot"]["observation"] == {
            "idle": True,
            "busy": False,
            "queued": 0,
        }
        await adapter.close()

    asyncio.run(run_fallback())

    # strata:model_loaded 0 during wait_ready fails closed
    prom_phase = 0

    def unloaded_prom_handler(request: httpx.Request) -> httpx.Response:
        nonlocal prom_phase
        if request.url.path == "/v1/models":
            return httpx.Response(200, json={"data": [{"id": "qwen36-local"}]})
        if request.url.path == "/health":
            return httpx.Response(
                200,
                json={"service": "strata", "loaded": True, "model": "qwen36-local", "max_context": 32768},
            )
        if request.url.path == "/v1/status":
            return httpx.Response(
                200,
                json={
                    "service": "strata",
                    "loaded": True,
                    "model": "qwen36-local",
                    "engine": "v0.1.45-poly.10",
                    "max_context": 32768,
                },
            )
        if request.url.path == "/metrics/prometheus":
            prom_phase += 1
            return httpx.Response(200, text=_prom_text(loaded=1 if prom_phase == 1 else 0))
        return httpx.Response(404)

    async def run_unloaded():
        adapter = LocalEngineAdapter(
            "http://127.0.0.1:48857",
            transport=httpx.MockTransport(unloaded_prom_handler),
            binding_verifier=_fake_binding,
        )
        await adapter.discover(cfg)
        ready = await adapter.wait_ready(seconds=0.5)
        assert ready is False
        assert adapter.native_uncertain is True
        await adapter.close()

    asyncio.run(run_unloaded())


def test_request_body_reasoning_effort_mapping(local_config) -> None:
    cfg_none = _polystrata_config(local_config, effort="none")
    assert LocalEngineAdapter.request_body(cfg_none, "hi")["reasoning_effort"] == "none"

    cfg_high = _polystrata_config(local_config, effort="high")
    assert LocalEngineAdapter.request_body(cfg_high, "hi")["reasoning_effort"] == "high"

    cfg_default = _polystrata_config(local_config, effort=None)
    assert LocalEngineAdapter.request_body(cfg_default, "hi")["reasoning_effort"] == "none"

    cfg_on = _polystrata_config(local_config, effort=None)
    cfg_on["generation"]["reasoning_mode"] = "on"
    assert LocalEngineAdapter.request_body(cfg_on, "hi")["reasoning_effort"] == "medium"

    # Non-polystrata family must not add reasoning_effort
    assert "reasoning_effort" not in LocalEngineAdapter.request_body(local_config, "hi")


def test_service_discover_identifies_polystrata_and_preserves_legacy_endpoints(
    monkeypatch: pytest.MonkeyPatch, capsys: pytest.CaptureFixture[str]
) -> None:
    processes = [
        {
            "pid": 100,
            "process_name": "python",
            "process_start_ticks": 11111,
            "listeners": [("127.0.0.1", 18080)],
        },
        {
            "pid": 101,
            "process_name": "python",
            "process_start_ticks": 22222,
            "listeners": [("127.0.0.1", 18081)],
        },
        {
            "pid": 102,
            "process_name": "llama-server",
            "process_start_ticks": 33333,
            "listeners": [("127.0.0.1", 8080)],
        },
    ]

    def handler(request: httpx.Request) -> httpx.Response:
        port = request.url.port
        path = request.url.path
        if port == 18080:
            if path == "/health":
                return httpx.Response(
                    200,
                    json={
                        "service": "strata",
                        "status": "ok",
                        "loaded": True,
                        "max_context": 65536,
                        "api_key": False,
                        "secret_field": "do-not-leak",
                    },
                )
            if path == "/v1/models":
                return httpx.Response(200, json={"data": [{"id": "qwen36-local"}]})
        if port == 18081:
            if path == "/health":
                return httpx.Response(
                    200,
                    json={"service": "strata", "loaded": False, "max_context": 32768},
                )
            if path == "/v1/models":
                return httpx.Response(200, json={"data": []})
        if port == 8080:
            if path == "/health":
                return httpx.Response(200, text="ok")
            if path == "/v1/models":
                return httpx.Response(200, json={"data": [{"id": "llama-3"}]})
        return httpx.Response(404)

    monkeypatch.setattr(discovery_app, "collect_listeners", lambda: (processes, ()))
    real_client = httpx.Client
    monkeypatch.setattr(
        discovery_app.httpx,
        "Client",
        lambda **kwargs: real_client(transport=httpx.MockTransport(handler), **kwargs),
    )

    code = cli.main(["service", "discover"])
    captured = capsys.readouterr()
    assert code == 0
    assert captured.err == ""
    assert "do-not-leak" not in captured.out

    result = json.loads(captured.out)
    jsonschema.validate(result, export_schema("service_discovery"))
    by_pid = {svc["pid"]: svc["endpoints"][0] for svc in result["details"]["services"]}

    assert by_pid[100]["engine_family"] == "polystrata"
    assert by_pid[100]["loaded"] is True
    assert by_pid[100]["max_context"] == 65536
    assert by_pid[100]["models"] == ["qwen36-local"]
    assert by_pid[100]["health"] == "2xx"

    assert by_pid[101]["engine_family"] == "polystrata"
    assert by_pid[101]["loaded"] is False
    assert by_pid[101]["max_context"] == 32768
    assert by_pid[101]["models"] is None
    assert by_pid[101]["reason"] == "models_empty"

    assert "engine_family" not in by_pid[102]
    assert by_pid[102]["models"] == ["llama-3"]
    assert by_pid[102]["health"] == "2xx"
