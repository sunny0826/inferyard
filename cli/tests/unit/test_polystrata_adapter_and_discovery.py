"""Unit tests for PolyStrata /status privacy sanitization and fail-closed queue probing."""

from __future__ import annotations

import asyncio
import json
from copy import deepcopy

import httpx
import pytest

from inferyard.adapters.local_engines.adapter import LocalEngineAdapter
from inferyard.contracts.schemas_observation import OBSERVATION
from inferyard.contracts.validation import validate_value
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


def _fake_binding(config):
    return {"pid": 500, "start_ticks": 600, "origin": config["endpoint"]["url"]}


def _fake_start_ticks(pid):
    return {500: 600}[pid]


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
            start_ticks=_fake_start_ticks,
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
            start_ticks=_fake_start_ticks,
        )
        try:
            with pytest.raises(PreflightError, match="polystrata_status_invalid"):
                await adapter.discover(cfg)
        finally:
            await adapter.close()

    asyncio.run(run_missing_queued())


def test_asset_scan_and_device_discovery_skip_polystrata_mtp_auxiliary_ggufs(tmp_path) -> None:
    from inferyard.platforms.asset_scan import is_polystrata_auxiliary_gguf, scan_assets
    from inferyard.platforms.device_preflight import discover_models

    models_root = tmp_path / "models"
    mtp_mac = models_root / "polystrata-data" / "mtp" / "mtp-q2_0.gguf"
    mtp_linux = models_root / "polystrata-data" / "models" / "gemma-4" / "MTP" / "mtp-gemma.gguf"
    pack_gguf = models_root / "polystrata-data" / "packs" / "q2_0" / "aux.gguf"
    real_gguf = models_root / "qwen-instruct.gguf"
    for target in (mtp_mac, mtp_linux, pack_gguf, real_gguf):
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(b"not-a-valid-gguf")

    assert is_polystrata_auxiliary_gguf(mtp_mac) is True
    assert is_polystrata_auxiliary_gguf(mtp_linux) is True
    assert is_polystrata_auxiliary_gguf(pack_gguf) is True
    assert is_polystrata_auxiliary_gguf(real_gguf) is False

    scanned = scan_assets([models_root])
    assert [m["path"] for m in scanned["models"]] == [str(real_gguf.resolve())]

    discovered = discover_models([models_root])
    assert [m["path"] for m in discovered] == [str(real_gguf.resolve())]

