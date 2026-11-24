"""Mock transport coverage for formal lab adapter safety/identity refusals."""

import asyncio

import httpx
import pytest

from inferyard.adapters.requests import request_body
from inferyard.config.loader import load_config
from inferyard.errors import PreflightError
from inferyard.registry import adapter_factory
from tests.lab_service import LabService, lab_config


@pytest.fixture(params=["kvmem", "ninfer"])
def lab_case(request, config_path):
    config = lab_config(load_config(config_path).config.to_dict(), request.param)
    return config, LabService(config)


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
