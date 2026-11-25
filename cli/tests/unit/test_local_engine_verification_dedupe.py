"""Local engines verify the bound service once per request, never back to back."""

import asyncio
from copy import deepcopy

from tests.local_engine_transport.conftest import rig  # noqa: F401


def _counting(state):
    calls = []

    def verifier(_config):
        calls.append(None)
        return deepcopy(state.binding)

    state.adapter._verify_binding = verifier
    return calls


def test_one_full_verification_per_request(rig):  # noqa: F811
    state = rig("vllm")
    calls = _counting(state)

    async def run():
        adapter = state.adapter
        await adapter.discover(state.config)
        after_discovery = len(calls)
        # Admission right after discovery reuses that verification.
        assert await adapter.wait_ready(5) is True
        assert len(calls) == after_discovery
        for request in range(1, 4):
            body = adapter.request_body(state.config, "hi")
            assert (await adapter.generate(body, 5))["execution_state"] == "completed"
            assert await adapter.wait_ready(5) is True  # residual after the request
            assert await adapter.wait_ready(5) is True  # admission of the next request
            assert len(calls) == after_discovery + request
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
        verified = len(calls)
        ticks[900] = 2  # same PID, new process incarnation
        assert await adapter.wait_ready(5) is False
        assert adapter.native_uncertain is True
        assert len(calls) == verified
        await adapter.close()

    asyncio.run(run())


def test_busy_reads_do_not_verify_until_idle(rig):  # noqa: F811
    state = rig("vllm")
    calls = _counting(state)
    busy = "vllm:num_requests_running 1\nvllm:num_requests_waiting 0\n"

    async def run():
        adapter = state.adapter
        await adapter.discover(state.config)
        body = adapter.request_body(state.config, "hi")
        await adapter.wait_ready(5)
        await adapter.generate(body, 5)
        before = len(calls)
        idle, state.metrics = state.metrics, busy
        loop = asyncio.get_running_loop()
        loop.call_later(0.1, lambda: setattr(state, "metrics", idle))
        assert await adapter.wait_ready(5) is True
        assert len(calls) == before + 1
        await adapter.close()

    asyncio.run(run())
