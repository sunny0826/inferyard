"""Small shared stages; callers retain their admission and cleanup policies."""

import asyncio
from dataclasses import dataclass

from inferyard.evidence.token_budgets import probe_budget
from inferyard.platforms.identity import PreflightError
from inferyard.runtime.duration_driver import run_duration
from inferyard.runtime.service_observation import (
    clean_observed,
    observation_manifest,
    observe_service,
)


@dataclass
class InitialIdle:
    pending: bool = False


async def prepare_service(
    adapter,
    config,
    store,
    lock,
    *,
    recovery_confirm=None,
    recovery_note=None,
    single_kind=None,
    idle_state=None,
):
    pre_idle_props = None
    if config["engine"]["adapter"] in ("kvmem", "ninfer"):
        pre_idle_props = await adapter.verify_properties(config)
        store.snapshot("service.props.json", pre_idle_props)
        store.snapshot("engine-capabilities.json", adapter.capability_evidence)
    recovery = None
    if recovery_confirm:
        recovery = lock.verify_manual_recovery(recovery_confirm, recovery_note, config["endpoint"])
    old_state = lock.state
    if old_state and old_state.get("dirty") and not recovery:
        if (old_state.get("server_pid"), old_state.get("process_start_ticks")) != (
            config["endpoint"]["server_pid"],
            config["endpoint"]["process_start_ticks"],
        ):
            raise PreflightError("dirty_service_requires_bound_recovery")
    if idle_state is not None:
        idle_state.pending = True
    if not await observe_service(adapter, config["execution"]["idle_wait_seconds"], store):
        if not old_state or not old_state.get("dirty"):
            options = {"kind": single_kind} if single_kind is not None else {}
            lock.dirty(store.run_id, config["endpoint"], "initial_idle_unknown", **options)
        raise PreflightError("initial_service_not_idle")
    clean_observed(lock, adapter, store, recovery=recovery)
    if idle_state is not None:
        idle_state.pending = False
    if recovery:
        store.snapshot("recovery.json", recovery)
    # Single historically re-queries falsey native properties; batch only re-queries None.
    if single_kind is not None:
        props = pre_idle_props or await adapter.verify_properties(config)
        if pre_idle_props is None:
            store.snapshot("service.props.json", props)
    elif pre_idle_props is None:
        store.snapshot("service.props.json", await adapter.verify_properties(config))


async def probe_service(
    execution,
    adapter,
    config,
    store,
    budgets,
    identity,
    *,
    output_budget_tokens=None,
    check_budget=None,
):
    for stream in (False, True):
        response = await execution.one("probe", config["execution"]["probe_prompt"], stream=stream)
        if response["execution_state"] != "completed":
            raise PreflightError("protocol_probe_failed")
        effective = await adapter.verify_effective(
            config, probe_budget(budgets)["input_tokens"], adapter.last_state
        )
        if output_budget_tokens is not None and store.strict_output:
            from inferyard.runtime.fixed_output import verify_probe

            effective["strict_output"] = verify_probe(effective, response, output_budget_tokens)
        store.snapshot("effective-stream.json" if stream else "effective-ordinary.json", effective)
        identity["effective_parameters"] = effective
        if check_budget is not None:
            check_budget()


async def warmup_and_baseline(execution, config, sampler, *, sleep=asyncio.sleep):
    for _ in range(config["execution"]["warmup_count"]):
        response = await execution.one("warmup", config["execution"]["warmup_prompt"])
        if response["execution_state"] != "completed":
            raise PreflightError("warmup_failed")
    sampler.set_phase("baseline")
    await sleep(config["telemetry"]["baseline_seconds"])


async def run_formal(execution, store, cases, *, duration_protocol=None, capacity_stop=None):
    if duration_protocol:
        by_id = {case["case_id"]: case for case in cases}

        async def request(index, cid, deadline, drain):
            execution.cancellation_reason = "duration_window_interrupted"
            return await execution.one(
                "formal", by_id[cid]["prompt"], index=index, admission_deadline_ns=deadline
            )

        def emit(kind, data):
            store.event(
                kind,
                "formal",
                None,
                data,
                monotonic_ns=data["start_ns" if kind == "duration_started" else "closed_ns"],
            )

        window = await run_duration(duration_protocol, store.selected, request, emit)
        if not window["window_completed"]:
            raise PreflightError(window["reason"])
    else:
        for index, case in enumerate(cases):
            terminal = await execution.one("formal", case["prompt"], index=index)
            if capacity_stop == "first_failed_request" and terminal["execution_state"] == "failed":
                raise PreflightError("capacity_scan_failed_request")


def seal_stopped(store, adapter, reason):
    store.event("run_stopped", "finalizing", None, {"reason": reason})
    if extra := observation_manifest(adapter):
        store.seal(extra)
    else:
        store.seal()
