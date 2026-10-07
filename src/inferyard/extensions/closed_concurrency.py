"""Separate closed-load protocol; the existing serial v1/v2 contract stays fixed."""

import asyncio
import time
from collections import Counter, deque

from inferyard.contracts.schemas_extensions import CLOSED_CONCURRENCY as SPEC
from inferyard.contracts.validation import ContractError, _validate
from inferyard.evidence.storage import EvidenceError
from inferyard.extensions.client_capacity import qualified


def validate_spec(spec):
    _validate(spec, SPEC, "closed_concurrency")
    if len(set(spec["case_ids"])) != len(spec["case_ids"]):
        raise ContractError("closed_concurrency.case_ids", "duplicate case")
    if spec["concurrency"] > spec["server_slots"]:
        raise ContractError("closed_concurrency.concurrency", "exceeds frozen server slots")


def reduce_closed(spec, rows, *, evidence_kind="fixture", client_capacity=None):
    validate_spec(spec)
    if evidence_kind not in ("fixture", "live"):
        raise EvidenceError("invalid_extension_evidence_kind")
    if [r.get("case_id") for r in rows] != spec["case_ids"]:
        raise EvidenceError("closed_case_order_or_denominator_mismatch")
    events, windows, ids, delays = [], [], set(), []
    counts = Counter()
    good = 0
    for row in rows:
        state = row.get("state")
        if state not in ("completed", "failed", "cancelled", "invalid", "not_executed"):
            raise EvidenceError("closed_terminal_invalid")
        counts[state] += 1
        if state == "not_executed":
            if any(row.get(k) is not None for k in ("request_id", "slot_id", "send_ns", "end_ns")):
                raise EvidenceError("closed_unexecuted_has_request")
            continue
        request = row.get("request_id")
        start, end, slot = (row.get(k) for k in ("send_ns", "end_ns", "slot_id"))
        if not isinstance(request, str) or not request or request in ids:
            raise EvidenceError("closed_request_identity_ambiguous")
        ids.add(request)
        if (
            type(start) is not int
            or type(end) is not int
            or not 0 <= start < end
            or type(slot) is not int
            or not 0 <= slot < spec["concurrency"]
        ):
            raise EvidenceError("closed_window_or_slot_invalid")
        delay = row.get("dispatch_delay_ns")
        if type(delay) is not int or delay < 0:
            raise EvidenceError("closed_dispatch_evidence_missing")
        delays.append(delay)
        if type(row.get("idle_confirmed")) is not bool or type(row.get("quality_pass")) not in (
            bool,
            type(None),
        ):
            raise EvidenceError("closed_idle_or_quality_evidence_invalid")
        if state == "completed" and row.get("protocol_complete") is not True:
            raise EvidenceError("closed_completed_protocol_missing")
        drained_at = row.get("drain_end_ns")
        if type(drained_at) is not int or drained_at < end:
            raise EvidenceError("closed_drain_window_missing")
        events.extend(((start, 1, slot), (drained_at, -1, slot)))
        windows.append((start, drained_at))
        good += int(
            state == "completed"
            and end - start <= spec["latency_limit_ms"] * 1_000_000
            and (not spec["require_quality"] or row["quality_pass"] is True)
        )
    active, peak = set(), 0
    for _, change, slot in sorted(events):
        if change == 1:
            if slot in active:
                raise EvidenceError("closed_slot_overlap")
            active.add(slot)
            peak = max(peak, len(active))
        else:
            if slot not in active:
                raise EvidenceError("closed_terminal_without_active_slot")
            active.remove(slot)
    duration = (max(b for _, b in windows) - min(a for a, _ in windows)) / 1e9 if windows else None
    idle = all(r.get("idle_confirmed") is True for r in rows if r["state"] != "not_executed")
    client_ok = bool(delays) and max(delays) <= spec["max_dispatch_delay_ns"]
    complete = counts["not_executed"] == 0 and counts["invalid"] == 0 and idle
    window = [min(a for a, _ in windows), max(b for _, b in windows)] if windows else None
    capacity_ok = qualified(client_capacity, window, spec)
    return {
        "definition": spec["definition"],
        "evidence_kind": evidence_kind,
        "planned": len(rows),
        "counts": {
            s: counts[s] for s in ("completed", "failed", "cancelled", "invalid", "not_executed")
        },
        "observed_peak_inflight": peak,
        "window_seconds": duration,
        "N01_completed_requests_per_second": counts["completed"] / duration if duration else None,
        "N02_goodput_per_second": good / duration if duration else None,
        "goodput_numerator": good,
        "client_dispatch_qualified": client_ok,
        "client_capacity_qualified": capacity_ok,
        "drained": idle,
        "complete": complete,
        "hardware_qualified": evidence_kind == "live" and complete and client_ok and capacity_ok,
        "N03_token_ITL": None,
        "N03_missing_reason": "no_verified_per_token_events",
        "resource_window": window,
        "limitations": [
            "closed_finite_request_cohort",
            "shared_process_resources_not_attributed_to_overlapping_requests",
            "not_open_arrival_load",
        ],
    }


async def run_closed(spec, adapter, emit, *, stop=None, clock=time.monotonic_ns):
    """Adapter owns each slot/connection; cancellation must subsequently prove idle.

    emit is synchronous and must durably save a started row before inference.
    Host locking, service identity and capability verification belong to the caller.
    """
    validate_spec(spec)
    stop = stop or asyncio.Event()
    queue = deque(enumerate(spec["case_ids"]))
    rows = [dict(case_id=c, state="not_executed") for c in spec["case_ids"]]
    slots = await adapter.slots()
    if len(slots) < spec["concurrency"] or any(s["is_processing"] for s in slots):
        raise EvidenceError("closed_verified_idle_slots_required")
    if [s.get("id") for s in slots[: spec["concurrency"]]] != list(range(spec["concurrency"])):
        raise EvidenceError("closed_slot_identity_invalid")

    async def worker(slot):
        available = clock()
        while queue and not stop.is_set():
            index, case = queue.popleft()
            start = clock()
            row = dict(
                case_id=case,
                request_id=f"closed-{index}",
                slot_id=slot,
                send_ns=start,
                dispatch_delay_ns=max(0, start - available),
                state="invalid",
                protocol_complete=False,
                quality_pass=None,
                idle_confirmed=False,
            )
            emit("request_started", row)
            task = asyncio.create_task(adapter.infer(case, slot))
            cancel = asyncio.create_task(stop.wait())
            try:
                done, _ = await asyncio.wait(
                    [task, cancel],
                    timeout=spec["timeout_seconds"],
                    return_when=asyncio.FIRST_COMPLETED,
                )
                if task in done:
                    result = task.result()
                    row.update(result)
                    if row.get("state") != "completed":
                        stop.set()
                else:
                    row.update(
                        state="cancelled" if stop.is_set() else "failed",
                        error="cancelled" if stop.is_set() else "timeout",
                    )
                    stop.set()
            except asyncio.CancelledError:
                row.update(state="cancelled", error="caller_cancelled")
                stop.set()
            except Exception:
                row.update(state="failed", error="adapter_failure")
                stop.set()
            finally:
                task.cancel()
                cancel.cancel()
                await asyncio.gather(task, cancel, return_exceptions=True)
                row["end_ns"] = max(clock(), start + 1)
                try:
                    row["idle_confirmed"] = await adapter.wait_idle(
                        slot, spec["drain_timeout_seconds"]
                    )
                except Exception:
                    row["idle_confirmed"] = False
                if not row["idle_confirmed"]:
                    stop.set()
                row["drain_end_ns"] = max(clock(), row["end_ns"])
                rows[index] = row
                emit("request_finished", row)
                available = clock()

    tasks = [asyncio.create_task(worker(i)) for i in range(spec["concurrency"])]
    try:
        async with asyncio.timeout(spec["max_wall_seconds"]):
            await asyncio.gather(*tasks)
    except TimeoutError:
        stop.set()
    finally:
        for task in tasks:
            if not task.done():
                task.cancel()
        await asyncio.gather(*tasks, return_exceptions=True)
    return rows
