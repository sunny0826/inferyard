"""Local engines fully verify the bound service once per run, not per request."""

import asyncio
from copy import deepcopy

import pytest

from inferyard.errors import PreflightError
from tests.local_engine_transport.conftest import rig  # noqa: F401


def _counting(state):
    calls = []

    def verifier(_config):
        calls.append(None)
        return deepcopy(state.binding)

    state.adapter._verify_binding = verifier
    return calls


async def _request(adapter, state):
    body = adapter.request_body(state.config, "hi")
    assert (await adapter.generate(body, 5))["execution_state"] == "completed"


def test_requests_reuse_the_discovery_verification(rig):  # noqa: F811
    state = rig("vllm")
    calls = _counting(state)

    async def run():
        adapter = state.adapter
        await adapter.discover(state.config)
        after_discovery = len(calls)
        assert await adapter.wait_ready(5) is True
        for _ in range(3):
            await _request(adapter, state)
            assert await adapter.wait_ready(5) is True  # residual after the request
            assert await adapter.wait_ready(5) is True  # admission of the next request
        assert len(calls) == after_discovery
        await adapter.verify_service(5)  # once, at the end of the run
        assert len(calls) == after_discovery + 1
        await adapter.close()

    asyncio.run(run())


def test_replaced_worker_blocks_admission_without_full_reverification(rig):  # noqa: F811
    state = rig("vllm")
    state.binding["workers"] = [{"pid": 900, "start_ticks": 1}]
    calls = _counting(state)
    ticks = {123: 456, 900: 1}
    state.start_ticks = lambda pid: ticks[pid]

    async def run():
        adapter = state.adapter
        await adapter.discover(state.config)
        assert await adapter.wait_ready(5) is True
        await _request(adapter, state)
        verified = len(calls)
        ticks[900] = 2  # same PID, new process incarnation
        assert await adapter.wait_ready(5) is False
        assert adapter.native_uncertain is True
        assert len(calls) == verified
        await adapter.close()

    asyncio.run(run())


def test_end_of_run_verification_catches_binding_drift(rig):  # noqa: F811
    state = rig("vllm")

    async def run():
        adapter = state.adapter
        await adapter.discover(state.config)
        assert await adapter.wait_ready(5) is True
        await _request(adapter, state)
        assert await adapter.wait_ready(5) is True
        state.binding["listener_identity"] = "another-listener"  # same incarnation, new binding
        with pytest.raises(PreflightError, match="local_engine_bound_service_changed"):
            await adapter.verify_service(5)
        await adapter.close()

    asyncio.run(run())
