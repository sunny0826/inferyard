"""Release protocol failures preserve uncertainty rather than inventing engine release."""

import asyncio

import httpx
import pytest

from inferyard.adapters.requests import request_body
from inferyard.config.loader import load_config
from inferyard.contracts.validation import Document
from inferyard.errors import ContractError, EvidenceError, PreflightError
from inferyard.registry import adapter_factory
from tests.lab_service import LabService, lab_config
from tests.native_service import NativeService


@pytest.fixture(params=["kvmem", "ninfer"])
def native_case(config_path, request):
    config = lab_config(load_config(config_path).config.to_dict(), request.param)
    config["engine"]["observation_mode"] = "auto"
    return config, NativeService(config)


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
