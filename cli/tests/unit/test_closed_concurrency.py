import asyncio

import pytest

from inferyard.errors import EvidenceError
from inferyard.extensions.closed_concurrency import reduce_closed, run_closed


def spec():
    return {
        "schema_version": 3,
        "definition": "closed_concurrency.v1",
        "case_ids": ["a", "b", "c", "d"],
        "concurrency": 2,
        "server_slots": 4,
        "timeout_seconds": 1,
        "drain_timeout_seconds": 0.1,
        "max_wall_seconds": 3,
        "latency_limit_ms": 100,
        "require_quality": True,
        "max_dispatch_delay_ns": 1_000_000_000,
        "max_client_cpu_fraction": 0.5,
    }


def rows():
    return [
        dict(
            case_id=c,
            request_id=c,
            slot_id=i % 2,
            send_ns=1 + i // 2 * 100,
            end_ns=101 + i // 2 * 100,
            drain_end_ns=101 + i // 2 * 100,
            state="completed",
            protocol_complete=True,
            quality_pass=i != 3,
            idle_confirmed=True,
            dispatch_delay_ns=1,
        )
        for i, c in enumerate(spec()["case_ids"])
    ]


class Adapter:
    def __init__(self, *, delay=0.002, idle=True):
        self.delay, self.idle, self.busy, self.peak, self.calls = delay, idle, set(), 0, []

    async def slots(self):
        return [{"id": i, "is_processing": False} for i in range(4)]

    async def infer(self, case, slot):
        assert slot not in self.busy
        self.busy.add(slot)
        self.calls.append(case)
        self.peak = max(self.peak, len(self.busy))
        try:
            await asyncio.sleep(self.delay)
            return {"state": "completed", "protocol_complete": True, "quality_pass": True}
        finally:
            self.busy.remove(slot)

    async def wait_idle(self, slot, timeout):
        return self.idle and slot not in self.busy


def test_handwritten_throughput_goodput_and_resource_window():
    result = reduce_closed(spec(), rows())
    assert result["window_seconds"] == 200 / 1e9
    assert result["N01_completed_requests_per_second"] == 20_000_000
    assert result["N02_goodput_per_second"] == 15_000_000
    assert result["resource_window"] == [1, 201]
    assert result["N03_token_ITL"] is None


def test_timeout_cancels_peers_keeps_unexecuted_denominator():
    plan = spec()
    plan["timeout_seconds"] = 0.002
    adapter = Adapter(delay=1)
    data = asyncio.run(run_closed(plan, adapter, lambda *a: None))
    result = reduce_closed(plan, data)
    assert sum(result["counts"].values()) == 4
    assert result["counts"]["not_executed"] == 2
    assert len(adapter.calls) == 2 and not adapter.busy
