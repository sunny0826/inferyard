"""Release protocol failures preserve uncertainty rather than inventing engine release."""

import asyncio
from types import SimpleNamespace

import httpx
import pytest

import inferyard.adapters.native_observation as observation
from inferyard.adapters.requests import request_body
from inferyard.config.loader import load_config
from inferyard.contracts.validation import ContractError, Document
from inferyard.evidence.storage import EvidenceError
from inferyard.platforms.identity import PreflightError
from inferyard.registry import adapter_factory
from tests.lab_service import LabService, lab_config
from tests.native_service import NativeService


@pytest.fixture(params=["kvmem", "ninfer"])
def native_case(config_path, request):
    config = lab_config(load_config(config_path).config.to_dict(), request.param)
    config["engine"]["observation_mode"] = "auto"
    return config, NativeService(config)


@pytest.mark.parametrize("scenario", ["timeout", "cancel", "sink"])
def test_native_interruption_keeps_dirty_scope_and_one_attempt(native_case, scenario):
    config, service = native_case
    service.options["delay"] = 0.05

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", transport=httpx.MockTransport(service)
        )
        try:
            await adapter.verify_properties(config)

            def emit(kind, data, now):
                if scenario == "sink" and kind == "http_response":
                    raise OSError("fixture evidence failure")

            task = asyncio.create_task(adapter.generate(request_body(config, "hello"), 0.01, emit))
            if scenario == "cancel":
                await asyncio.sleep(0.001)
                task.cancel()
                with pytest.raises(asyncio.CancelledError):
                    await task
            elif scenario == "sink":
                with pytest.raises(EvidenceError, match="event_sink_failed"):
                    await task
            else:
                result = await task
                assert result["error_category"] == "total_timeout"
            assert adapter.native_uncertain
            assert not await adapter.wait_ready()
            assert adapter.native_cancellation["native_cancel"] is None
        finally:
            await adapter.close()

    asyncio.run(run())
    assert len(service.requests) == 1
    assert not any(call.url.path.endswith("/cancel") for call in service.calls)


def test_partial_lab_capabilities_default_to_disclosed_native(native_case):
    config, _ = native_case
    service = LabService(config)
    service.identity_value["capabilities"]["token_budget"] = False

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", transport=httpx.MockTransport(service)
        )
        try:
            await adapter.verify_properties(config)
            assert adapter.observation_mode == "native"
            assert (await adapter.token_budget(config, "hello"))["input_tokens"] is None
            assert adapter.capability_evidence["engine_internal_drain"]["value"] is None
        finally:
            await adapter.close()

    asyncio.run(run())


def test_auto_never_downgrades_conflicting_lab_engine_identity(native_case):
    config, _ = native_case
    service = LabService(config)
    service.identity_value["engine"] = (
        "ninfer" if config["engine"]["adapter"] == "kvmem" else "kvmem"
    )

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", transport=httpx.MockTransport(service)
        )
        try:
            with pytest.raises(PreflightError, match="lab_engine_mismatch"):
                await adapter.verify_properties(config)
        finally:
            await adapter.close()

    asyncio.run(run())
    assert not service.requests


@pytest.mark.parametrize(
    "field,value",
    [("observation_mode", "assume_idle"), ("environment", {"NINFER_API_KEY": "secret"})],
)
def test_observation_config_rejects_unsupported_mode_or_environment(native_case, field, value):
    config, _ = native_case
    config["engine"][field] = value
    with pytest.raises(ContractError):
        Document.parse("config", config)


def test_capability_discovery_has_seven_individual_five_second_budgets(native_case, monkeypatch):
    config, _ = native_case
    clock, seen = [100.0], []
    monkeypatch.setattr(observation, "time", SimpleNamespace(monotonic=lambda: clock[0]))

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])("http://127.0.0.1:8080")

        async def inspect(path, *, deadline):
            assert deadline == clock[0] + 5
            seen.append(path)
            clock[0] = deadline
            return {
                "available": None,
                "missing_reason": "http_404",
                "status_code": 404,
                "body": None,
            }

        monkeypatch.setattr(adapter, "inspect_endpoint", inspect)
        try:
            await adapter.discover(config)
        finally:
            await adapter.close()

    asyncio.run(run())
    assert tuple(seen) == observation.PATHS and clock[0] == 135
