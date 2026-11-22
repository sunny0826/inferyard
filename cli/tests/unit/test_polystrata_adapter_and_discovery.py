"""Unit tests for PolyStrata HTTP protocol adapter, evidence replay, and service discovery."""

from __future__ import annotations

import asyncio
import hashlib
import json
from copy import deepcopy
from pathlib import Path
from types import SimpleNamespace

import httpx
import jsonschema
import pytest

from inferyard import cli
from inferyard.adapters.local_engines.adapter import LocalEngineAdapter
from inferyard.application import service_discovery as discovery_app
from inferyard.contracts.schemas import export_schema
from inferyard.contracts.schemas_observation import OBSERVATION
from inferyard.contracts.validation import json_bytes, validate_value
from inferyard.errors import PreflightError
from inferyard.evidence.local_engines import verify_queue_events
from inferyard.evidence.storage import EvidenceStore, verify_manifest
from inferyard.platforms.identity import hash_file
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


def test_polystrata_discover_aliases_wait_ready_prometheus_and_streaming_generate(
    local_config,
) -> None:
    cfg = _polystrata_config(local_config, context_size=32768, effort="none")
    prom_calls = 0
    posted_bodies: list[dict] = []

    def handler(request: httpx.Request) -> httpx.Response:
        nonlocal prom_calls
        path = request.url.path
        if path == "/v1/models":
            return httpx.Response(
                200,
                json={
                    "object": "list",
                    "data": [
                        {"id": "qwen36-local", "object": "model"},
                        {
                            "id": "qwen36-alias",
                            "object": "model",
                            "alias_of": "qwen36-local",
                        },
                    ],
                },
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
        validate_value(props["observation"], OBSERVATION, "engine_observation")
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


def test_polystrata_discover_rejects_unloaded_mismatches_and_zero_status_context(
    local_config,
) -> None:
    cfg = _polystrata_config(local_config, context_size=32768)

    def make_handler(models_payload, health_payload, status_payload):
        def handler(request: httpx.Request) -> httpx.Response:
            if request.url.path == "/v1/models":
                return httpx.Response(200, json=models_payload)
            if request.url.path == "/health":
                return httpx.Response(200, json=health_payload)
            if request.url.path == "/v1/status":
                return httpx.Response(200, json=status_payload)
            if request.url.path == "/metrics/prometheus":
                return httpx.Response(200, text=_prom_text())
            return httpx.Response(404)

        return handler

    base_models = {"data": [{"id": "qwen36-local"}]}
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
        "cache_max_tokens": 32768,
    }

    cases = [
        (base_models, {**base_health, "loaded": False}, base_status, "polystrata_model_not_loaded"),
        (
            base_models,
            {**base_health, "service": "other"},
            base_status,
            "polystrata_service_unverified",
        ),
        (
            base_models,
            base_health,
            {**base_status, "cache_max_tokens": 0},
            "polystrata_context_mismatch",
        ),
        (base_models, base_health, {**base_status, "engine": None}, "engine_version_unverified"),
        (
            {"data": [{"id": "qwen36-local"}, {"id": "other-model"}]},
            base_health,
            base_status,
            "served_model_unverified",
        ),
    ]

    async def run_one(models_p, health_p, status_p, expected_err):
        adapter = LocalEngineAdapter(
            "http://127.0.0.1:48857",
            transport=httpx.MockTransport(make_handler(models_p, health_p, status_p)),
            binding_verifier=_fake_binding,
        )
        try:
            with pytest.raises(PreflightError, match=expected_err):
                await adapter.discover(cfg)
        finally:
            await adapter.close()

    for models_p, health_p, status_p, expected_err in cases:
        asyncio.run(run_one(models_p, health_p, status_p, expected_err))


def test_polystrata_status_fallback_sanitizes_tail_and_rejects_missing_queued(
    local_config,
) -> None:
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
                json={
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
                    "loaded": True,
                    "model": "qwen36-local",
                    "engine": "v0.1.45-poly.10",
                    "cache_max_tokens": 32768,
                },
            )
        if path == "/metrics/prometheus":
            return httpx.Response(404)
        if path == "/status":
            status_calls += 1
            if status_calls == 2:
                return httpx.Response(
                    200,
                    json={"busy": True, "queued": 1, "tail": "PRIVATE_OTHER_REQUEST_TEXT"},
                )
            return httpx.Response(
                200,
                json={"busy": False, "queued": 0, "tail": "PRIVATE_OTHER_REQUEST_TEXT"},
            )
        return httpx.Response(404)

    async def run_fallback():
        adapter = LocalEngineAdapter(
            "http://127.0.0.1:48857",
            transport=httpx.MockTransport(status_only_handler),
            binding_verifier=_fake_binding,
        )
        cap = await adapter.discover(cfg)
        validate_value(cap, OBSERVATION, "engine_observation")
        assert "PRIVATE_OTHER_REQUEST_TEXT" not in json.dumps(cap)

        events: list[dict] = []
        ready = await adapter.wait_ready(seconds=2.0, observe=lambda _s, ev: events.append(ev))
        assert ready is True
        assert len(events) == 2
        assert "PRIVATE_OTHER_REQUEST_TEXT" not in json.dumps(events)
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

    # Missing queued in /status must fail closed (never default to 0)
    def missing_queued_handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == "/v1/models":
            return httpx.Response(200, json={"data": [{"id": "qwen36-local"}]})
        if request.url.path == "/health":
            return httpx.Response(
                200,
                json={
                    "service": "strata",
                    "loaded": True,
                    "model": "qwen36-local",
                    "max_context": 32768,
                },
            )
        if request.url.path == "/v1/status":
            return httpx.Response(
                200,
                json={
                    "service": "strata",
                    "loaded": True,
                    "model": "qwen36-local",
                    "engine": "v0.1.45-poly.10",
                    "cache_max_tokens": 32768,
                },
            )
        if request.url.path == "/metrics/prometheus":
            return httpx.Response(404)
        if request.url.path == "/status":
            return httpx.Response(200, json={"busy": False})
        return httpx.Response(404)

    async def run_missing_queued():
        adapter = LocalEngineAdapter(
            "http://127.0.0.1:48857",
            transport=httpx.MockTransport(missing_queued_handler),
            binding_verifier=_fake_binding,
        )
        try:
            with pytest.raises(PreflightError, match="polystrata_status_invalid"):
                await adapter.discover(cfg)
        finally:
            await adapter.close()

    asyncio.run(run_missing_queued())


@pytest.mark.parametrize("queue_mode", ["prometheus", "status"])
def test_polystrata_evidence_seal_and_offline_queue_verify(
    local_config, tmp_path: Path, queue_mode: str
) -> None:
    """Both /metrics/prometheus and /status paths seal cleanly and pass offline replay."""
    root = tmp_path.resolve()
    ps_root = root / "polystrata"
    (ps_root / "serve").mkdir(parents=True)
    exe = ps_root / ".venv" / "bin" / "python"
    exe.parent.mkdir(parents=True)
    exe.write_bytes(b"python")
    server_py = ps_root / "serve" / "server.py"
    server_py.write_text("print('ok')\n", encoding="utf-8")
    model_file = root / "qwen36.gguf"
    model_file.write_bytes(b"gguf-bytes")
    template_file = root / "chat-template.jinja"
    template_file.write_text("{{ messages }}", encoding="utf-8")
    ps_cfg_file = root / "strata-candidate.json"
    ps_cfg_file.write_text(
        json.dumps(
            {
                "exe": str(ps_root / "engine-poly" / "strata-poly"),
                "args": ["--model", str(model_file), "--max-context", "32768"],
                "cwd": str(ps_root),
                "model_name": "qwen36-local",
            }
        ),
        encoding="utf-8",
    )

    exe_id = hash_file(exe)
    model_id = hash_file(model_file)
    ps_cfg_id = hash_file(ps_cfg_file)
    tpl_id = hash_file(template_file)

    runtime_doc = {
        "schema_version": 3,
        "definition": "local-engine-runtime.v1",
        "installation": {
            "schema_version": 3,
            "definition": "local-engine-installation.v1",
            "family": "polystrata",
            "executable": str(exe),
            "working_directory": str(ps_root),
            "runtime_roots": [str(ps_root)],
            "environment": {"PYTHONDONTWRITEBYTECODE": "1"},
        },
        "python": None,
        "unresolved_libraries": [],
        "roots": [
            {
                "path": str(ps_root),
                "entries": [
                    {
                        "path": ".venv/bin/python",
                        "sha256": exe_id.sha256,
                        "size": exe_id.size,
                        "symlink": None,
                    }
                ],
            }
        ],
    }
    runtime_path = root / "installation.json"
    runtime_path.write_bytes(json_bytes(runtime_doc))
    runtime_id = hash_file(runtime_path)

    assets_files = [{"path": model_file.name, "sha256": model_id.sha256, "size": model_id.size}]
    assets_doc = {
        "kind": "gguf",
        "path": str(model_file),
        "sha256": hashlib.sha256(json_bytes(assets_files)).hexdigest(),
        "files": assets_files,
    }
    assets_path = root / "model-assets.json"
    assets_path.write_bytes(json_bytes(assets_doc))
    assets_id = hash_file(assets_path)

    startup_args = [
        str(server_py),
        "--engine",
        "strata",
        "--config",
        str(ps_cfg_file),
        "--host",
        "127.0.0.1",
        "--port",
        "48857",
    ]
    raw_argv = b"\0".join(arg.encode("utf-8") for arg in [str(exe), *startup_args]) + b"\0"
    model_stat = model_file.stat()
    binding_doc = {
        "schema_version": 3,
        "definition": "local-engine-binding.v1",
        "installation_sha256": runtime_id.sha256,
        "service": {
            "pid": 500,
            "start_ticks": 12345,
            "origin": "http://127.0.0.1:48857",
            "address": "127.0.0.1",
            "port": 48857,
            "executable_sha256": exe_id.sha256,
            "argv_sha256": hashlib.sha256(raw_argv).hexdigest(),
            "cwd_sha256": hashlib.sha256(str(ps_root).encode("utf-8")).hexdigest(),
            "listener_inode": None,
            "listener_identity": "tcp://127.0.0.1:48857/pid:500",
            "listener_source": "psutil:TCP:LISTEN:pid",
            "model_binding": {
                "engine": "polystrata",
                "path": str(model_file),
                "device": model_stat.st_dev,
                "inode": model_stat.st_ino,
                "source": "verified_startup_file_identity",
            },
            "workers": [],
        },
    }
    binding_path = root / "binding.json"
    binding_path.write_bytes(json_bytes(binding_doc))
    binding_id = hash_file(binding_path)

    cfg = _polystrata_config(local_config, context_size=32768, effort="none")
    cfg["model"].update(
        local_path=str(model_file),
        assets_path=str(assets_path),
        assets_sha256=assets_id.sha256,
        content_sha256=assets_doc["sha256"],
        template_path=str(template_file),
        template_sha256=tpl_id.sha256,
    )
    cfg["engine"].update(
        working_directory=str(ps_root),
        installation_path=str(runtime_path),
        installation_sha256=runtime_id.sha256,
        polystrata_config_path=str(ps_cfg_file),
        polystrata_config_sha256=ps_cfg_id.sha256,
        startup_args=startup_args,
    )
    cfg["endpoint"] = {
        "url": "http://127.0.0.1:48857",
        "server_pid": 500,
        "process_start_ticks": 12345,
        "binding_path": str(binding_path),
        "binding_sha256": binding_id.sha256,
    }

    def handler(request: httpx.Request) -> httpx.Response:
        path = request.url.path
        if path == "/v1/models":
            return httpx.Response(200, json={"data": [{"id": "qwen36-local"}]})
        if path == "/health":
            return httpx.Response(
                200,
                json={
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
                    "loaded": True,
                    "model": "qwen36-local",
                    "engine": "v0.1.45-poly.10",
                    "cache_max_tokens": 32768,
                },
            )
        if path == "/metrics/prometheus":
            if queue_mode == "prometheus":
                return httpx.Response(200, text=_prom_text(processing=0, deferred=0, loaded=1))
            return httpx.Response(404)
        if path == "/status":
            return httpx.Response(200, json={"busy": False, "queued": 0, "tail": "strip-me"})
        return httpx.Response(404)

    async def collect():
        adapter = LocalEngineAdapter(
            "http://127.0.0.1:48857",
            transport=httpx.MockTransport(handler),
            binding_verifier=lambda _c: deepcopy(binding_doc["service"]),
        )
        props = await adapter.verify_properties(cfg)
        observed_events: list[dict] = []
        ready = await adapter.wait_ready(
            seconds=1.0, observe=lambda _s, ev: observed_events.append(ev)
        )
        assert ready is True
        await adapter.close()
        return props, observed_events

    props, observed_events = asyncio.run(collect())
    store = EvidenceStore(root / "runs", run_id=f"run-{queue_mode}")
    try:
        store.seal({"engine_observation": props["observation"]})
    finally:
        store.close()
    assert (store.path / "manifest.json").is_file()
    assert verify_manifest(store.path) == []

    file_records = [
        {"path": item.path, "sha256": item.sha256, "size": item.size}
        for item in (runtime_id, binding_id, assets_id, tpl_id, ps_cfg_id, model_id)
    ]
    observations = {
        "identity": {
            "origin": cfg["endpoint"]["url"],
            "process": binding_doc["service"],
            "environment": {},
            "verification": "verified",
            "local_engine": {
                "runtime": runtime_doc,
                "binding": binding_doc,
                "model_assets": assets_doc,
            },
            "files": file_records,
        },
        "engine_capabilities": props["observation"],
        "service_properties": props,
    }
    reads = SimpleNamespace(
        observation=lambda kind, stage=None, key=None: observations[kind],
        observation_keys=lambda kind, stage=None: [],
    )
    events = [
        {"event_type": "native_observed", "request_id": None, "data": observed_events[0]},
        {"event_type": "run_stopped", "request_id": None, "data": {"reason": "plan_finished"}},
    ]
    verify_queue_events(store.path, cfg, events, reads=reads)


def test_request_body_reasoning_effort_mapping(local_config) -> None:
    cfg_none = _polystrata_config(local_config, effort="none")
    assert LocalEngineAdapter.request_body(cfg_none, "hi")["reasoning_effort"] == "none"

    cfg_high = _polystrata_config(local_config, effort="high")
    assert LocalEngineAdapter.request_body(cfg_high, "hi")["reasoning_effort"] == "high"

    cfg_default = _polystrata_config(local_config, effort=None)
    assert LocalEngineAdapter.request_body(cfg_default, "hi")["reasoning_effort"] == "none"

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
