"""Mock transport coverage for actual formal adapter failures and legacy compatibility."""

import asyncio
import json
from copy import deepcopy

import httpx
import pytest

from inferyard.adapters.kvmem import KVMemAdapter
from inferyard.adapters.ninfer import NInferAdapter
from inferyard.adapters.requests import request_body
from inferyard.config.loader import load_config
from inferyard.contracts.validation import ContractError, Document
from inferyard.platforms.identity import PreflightError
from inferyard.registry import adapter_collector_id, adapter_factory
from tests.lab_service import LabService, lab_config


@pytest.fixture(params=["kvmem", "ninfer"])
def lab_case(request, config_path):
    config = lab_config(load_config(config_path).config.to_dict(), request.param)
    return config, LabService(config)


def test_registry_keeps_distinct_engine_ids(lab_case):
    config, _ = lab_case
    engine = config["engine"]["adapter"]
    assert adapter_factory(engine) is {"kvmem": KVMemAdapter, "ninfer": NInferAdapter}[engine]
    assert adapter_collector_id(engine, batch=True) == "windows-memory.v1"
    body = request_body(config, "hello")
    assert "chat_template_kwargs" not in body and "cache_prompt" not in body
    assert Document.parse("config", config).to_dict()["schema_version"] == 3


@pytest.mark.parametrize("stream", [False, True])
def test_complete_request_budget_parameters_and_release(lab_case, stream):
    config, service = lab_case

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", transport=httpx.MockTransport(service)
        )
        try:
            await adapter.verify_properties(config)
            assert await adapter.wait_idle()
            budget = await adapter.token_budget(config, "hello")
            assert budget["input_tokens"] == 1
            events = []
            result = await adapter.generate(
                request_body(config, "hello", stream=stream), 1, lambda *args: events.append(args)
            )
            assert result["execution_state"] == "completed"
            assert result["content"] == "北京"
            assert adapter.pending_request is not None
            await adapter.verify_effective(config, 1, adapter.last_state)
            assert await adapter.wait_idle()
            assert adapter.pending_request is None
            usage = next(data for kind, data, _ in events if kind == "lab_usage")
            assert usage["cached_tokens"] is None
            assert usage["usage_missing_reasons"]["cached_tokens"] == "not_reported"
            assert adapter.capability_evidence["mode"] == "lab"
            assert not any(call.url.path == "/monitor" for call in service.calls)
        finally:
            await adapter.close()

    asyncio.run(run())


@pytest.mark.parametrize(
    "option,reason",
    [
        ("truncated", "lab_generation_done"),
        ("mismatched_id", "lab_generation_id"),
        ("after_done", "lab_generation_done"),
        ("http_error", "http_error"),
    ],
)
def test_bad_response_has_one_attempt_and_request_cancel(lab_case, option, reason):
    config, service = lab_case
    service.options[option] = True

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", transport=httpx.MockTransport(service)
        )
        try:
            await adapter.verify_properties(config)
            result = await adapter.generate(request_body(config, "hello"), 1)
            assert result["execution_state"] == "failed"
            assert result["error_category"] == reason
            assert adapter.pending_request is not None
            assert await adapter.wait_idle()
        finally:
            await adapter.close()

    asyncio.run(run())
    assert sum(call.url.path == "/v1/chat/completions" for call in service.calls) == 1
    assert sum(call.url.path.endswith("/cancel") for call in service.calls) == 1


@pytest.mark.parametrize("path", ["/lab/v1/identity", "/lab/v1/token-budget"])
def test_missing_required_protocol_is_contract_error(lab_case, path):
    config, service = lab_case
    service.options["missing"] = [path]

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", transport=httpx.MockTransport(service)
        )
        try:
            with pytest.raises(PreflightError, match="lab_contract_http_error"):
                await adapter.token_budget(config, "hello")
        finally:
            await adapter.close()

    asyncio.run(run())
    assert not service.requests


def test_decline_sends_nothing_and_preserves_no_partial_state(lab_case):
    config, service = lab_case

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", transport=httpx.MockTransport(service)
        )
        try:
            await adapter.verify_properties(config)
            assert (
                await adapter.generate(
                    request_body(config, "hello"), 1, before_send=lambda _: False
                )
                is None
            )
            assert adapter.last_state is None
        finally:
            await adapter.close()

    asyncio.run(run())
    assert not service.requests


@pytest.mark.parametrize(
    "change", ["kind", "size", "manifest", "cache", "seed", "working_directory", "ledger"]
)
def test_contract_refuses_bad_asset_combinations(lab_case, change):
    config, _ = lab_case
    bad = deepcopy(config)
    if change == "kind":
        bad["model"]["kind"] = "ninfer" if bad["model"]["kind"] == "gguf" else "gguf"
    elif change == "size":
        bad["model"]["bytes"] = True
    elif change == "manifest":
        bad["engine"]["asset_manifest"].pop(0)
    elif change == "cache":
        bad["conditions"]["cache_policy"] = "enabled"
    elif change == "seed":
        bad["generation"]["seed_support"] = "unknown"
    elif change == "working_directory":
        bad["engine"].pop("working_directory")
    elif change == "ledger":
        if bad["engine"]["adapter"] == "kvmem":
            bad["engine"]["asset_manifest"].append(dict(bad["engine"]["asset_manifest"][0]))
        else:
            bad["model"].pop("component_ledger_path")
    for kind in ("config", "config_input"):
        with pytest.raises(ContractError):
            Document.parse(kind, bad)


@pytest.mark.parametrize("scenario", ["timeout", "cancel", "sink", "cancel_error"])
def test_timeout_cancellation_and_sink_failure_preserve_pending_id(lab_case, scenario):
    from inferyard.evidence.storage import EvidenceError

    config, service = lab_case
    service.options["delay"] = 0.05
    if scenario == "cancel_error":
        service.options["cancel_error"] = True

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", transport=httpx.MockTransport(service)
        )
        try:
            await adapter.verify_properties(config)

            def emit(*args):
                pass

            if scenario == "sink":

                def emit(kind, data, now):
                    if kind == "http_response":
                        raise OSError("fixture evidence failure")

            task = asyncio.create_task(
                adapter.generate(
                    request_body(config, "hello"),
                    0.01 if scenario in ("timeout", "cancel_error") else 1,
                    emit,
                )
            )
            if scenario == "cancel":
                await asyncio.sleep(0.01)
                task.cancel()
                with pytest.raises(asyncio.CancelledError):
                    await task
            elif scenario == "sink":
                with pytest.raises(EvidenceError, match="event_sink_failed"):
                    await task
            else:
                result = await task
                assert result["execution_state"] == "failed"
                assert result["error_category"] == (
                    "total_timeout_cancel_unconfirmed"
                    if scenario == "cancel_error"
                    else "total_timeout"
                )
            assert adapter.pending_request is not None
            assert sum(call.url.path.endswith("/cancel") for call in service.calls) == 1
        finally:
            await adapter.close()

    asyncio.run(run())
    assert len(service.requests) == 1


@pytest.mark.parametrize(
    "scenario", ["poisoned", "busy", "instance", "capability", "effective", "tokens"]
)
def test_false_idle_identity_budget_and_effective_settings_are_refused(lab_case, scenario):
    config, service = lab_case

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", transport=httpx.MockTransport(service)
        )
        try:
            if scenario == "capability":
                service.identity_value["capabilities"]["lifecycle"] = False
                with pytest.raises(PreflightError, match="capability_missing"):
                    await adapter.verify_properties(config)
                return
            await adapter.verify_properties(config)
            if scenario == "poisoned":
                service.options["poisoned"] = True
                assert not await adapter.wait_idle(0.03)
            elif scenario == "instance":
                service.identity_value["server_instance_id"] = "2" * 32
                with pytest.raises(PreflightError, match="identity_changed"):
                    await adapter.wait_idle()
            elif scenario == "tokens":
                service.options["input_tokens"] = config["conditions"]["context_size"]
                with pytest.raises(PreflightError, match="context_budget_exceeded"):
                    await adapter.token_budget(config, "hello")
            else:
                await adapter.generate(request_body(config, "hello"), 1)
                if scenario == "effective":
                    service.options["effective_mismatch"] = True
                    with pytest.raises(PreflightError, match="effective_parameter_mismatch"):
                        await adapter.verify_effective(config, 1, adapter.last_state)
                else:
                    service.options["busy"] = True
                    assert not await adapter.wait_idle(0.03)
        finally:
            await adapter.close()

    asyncio.run(run())


def test_lab_wire_and_terminal_redact_echoed_credentials_across_chunks(lab_case):
    from tests.lab_service import Stream

    config, service = lab_case
    secret = "fixture-secret-987654321"
    service.options["answer"] = "北京 " + secret
    original = service.__call__

    def handler(request):
        response = original(request)
        if request.url.path == "/v1/chat/completions":
            # Read the synthetic stream's bytes and split inside the credential.
            raw = response.stream.chunks[0]
            at = raw.index(secret.encode()) + 10
            return httpx.Response(200, stream=Stream([raw[:at], raw[at:]]))
        return response

    async def run():
        adapter = adapter_factory(config["engine"]["adapter"])(
            "http://127.0.0.1:8080", secret=secret, transport=httpx.MockTransport(handler)
        )
        try:
            await adapter.verify_properties(config)
            events = []
            result = await adapter.generate(
                request_body(config, "hello"), 1, lambda *args: events.append(args)
            )
            assert result["execution_state"] == "completed"
            assert secret not in json.dumps(events) + json.dumps(result)
            assert adapter.last_state.redacted
        finally:
            await adapter.close()

    asyncio.run(run())


def test_committed_legal_and_illegal_samples(config_path):
    from pathlib import Path

    from inferyard.contracts.validation import strict_json_loads

    root = Path(__file__).parents[1] / "fixtures/contracts"
    for engine in ("kvmem", "ninfer"):
        valid = strict_json_loads((root / f"config.{engine}.valid.json").read_text())
        assert Document.parse("config", valid).to_dict() == valid
        bad = strict_json_loads((root / f"config.{engine}.wrong-asset.json").read_text())
        with pytest.raises(ContractError, match="kind"):
            Document.parse("config", bad)
        bad = strict_json_loads((root / f"config.{engine}.wrong-observation.json").read_text())
        with pytest.raises(ContractError, match="observation_mode"):
            Document.parse("config", bad)
