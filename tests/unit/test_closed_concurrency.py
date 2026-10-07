import asyncio

import pytest

from inferyard.evidence.storage import EvidenceError
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


def test_scheduler_binds_slots_preserves_order_and_never_retries():
    adapter = Adapter()
    events = []
    result = asyncio.run(run_closed(spec(), adapter, lambda *a: events.append(a)))
    assert adapter.peak == 2 and len(adapter.calls) == len(set(adapter.calls)) == 4
    assert [r["case_id"] for r in result] == spec()["case_ids"]
    assert len(events) == 8
    summary = reduce_closed(spec(), result)
    assert summary["complete"] and summary["observed_peak_inflight"] == 2
    assert not summary["hardware_qualified"]


def test_handwritten_throughput_goodput_and_resource_window():
    result = reduce_closed(spec(), rows())
    assert result["window_seconds"] == 200 / 1e9
    assert result["N01_completed_requests_per_second"] == 20_000_000
    assert result["N02_goodput_per_second"] == 15_000_000
    assert result["resource_window"] == [1, 201]
    assert result["N03_token_ITL"] is None


def test_drain_is_part_of_throughput_window_and_client_capacity_is_required():
    data = rows()
    data[-1]["drain_end_ns"] += 100
    result = reduce_closed(spec(), data, evidence_kind="live")
    assert result["window_seconds"] == 300 / 1e9
    assert result["resource_window"] == [1, 301]
    assert not result["hardware_qualified"]
    proof = dict(
        begin_ns=0,
        end_ns=400,
        client_pid=10,
        process_start_ticks=1,
        clock_id="fixture-clock",
        process_cpu_ns=100,
        event_loop_samples=2,
        max_event_loop_lag_ns=1,
    )
    result = reduce_closed(spec(), data, evidence_kind="live", client_capacity=proof)
    assert result["client_capacity_qualified"] and result["hardware_qualified"]
    proof["process_cpu_ns"] = 300
    assert not reduce_closed(spec(), data, evidence_kind="live", client_capacity=proof)[
        "hardware_qualified"
    ]
    proof["process_cpu_ns"] = 100
    proof["event_loop_samples"] = 0
    assert not reduce_closed(spec(), data, evidence_kind="live", client_capacity=proof)[
        "hardware_qualified"
    ]


@pytest.mark.parametrize(
    "change,reason",
    [
        ({"request_id": "a"}, "identity_ambiguous"),
        ({"slot_id": 0}, "slot_overlap"),
        ({"slot_id": True}, "window_or_slot"),
        ({"end_ns": 0}, "window_or_slot"),
        ({"protocol_complete": False}, "protocol_missing"),
        ({"dispatch_delay_ns": None}, "dispatch_evidence"),
    ],
)
def test_malformed_evidence_does_not_grant_qualification(change, reason):
    data = rows()
    data[1].update(change)
    with pytest.raises(EvidenceError, match=reason):
        reduce_closed(spec(), data)


def test_missing_idle_and_client_delay_refuse_real_qualification():
    data = rows()
    data[1]["idle_confirmed"] = False
    data[0]["dispatch_delay_ns"] = 2_000_000_000
    result = reduce_closed(spec(), data, evidence_kind="live")
    assert not result["hardware_qualified"] and not result["complete"]
    assert not result["client_dispatch_qualified"]


def test_timeout_cancels_peers_keeps_unexecuted_denominator():
    plan = spec()
    plan["timeout_seconds"] = 0.002
    adapter = Adapter(delay=1)
    data = asyncio.run(run_closed(plan, adapter, lambda *a: None))
    result = reduce_closed(plan, data)
    assert sum(result["counts"].values()) == 4
    assert result["counts"]["not_executed"] == 2
    assert len(adapter.calls) == 2 and not adapter.busy


def test_unknown_idle_stops_new_requests_without_retry():
    adapter = Adapter(idle=False)
    data = asyncio.run(run_closed(spec(), adapter, lambda *a: None))
    assert len(adapter.calls) == 2
    assert not reduce_closed(spec(), data)["drained"]


def test_preexisting_busy_slot_refuses_all_requests():
    adapter = Adapter()

    async def busy():
        return [{"id": 0, "is_processing": True}]

    adapter.slots = busy
    with pytest.raises(EvidenceError, match="verified_idle"):
        asyncio.run(run_closed(spec(), adapter, lambda *a: None))
    assert adapter.calls == []
