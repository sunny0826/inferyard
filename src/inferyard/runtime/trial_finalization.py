"""Seal trial evidence while preserving single and batch cleanup differences."""

import asyncio
import time

from inferyard import SCHEMA_VERSION
from inferyard.contracts.validation import ContractError
from inferyard.evidence.error_reasons import safe_reason
from inferyard.evidence.storage import EvidenceError
from inferyard.runtime.trial_lifecycle import seal_stopped


async def finish_trial(
    store,
    adapter,
    sampler,
    sampler_task,
    identity,
    environment,
    code,
    reason,
    *,
    lock,
    locked,
    safety,
    started_at,
    allowed_seconds,
    single=False,
):
    if sampler:
        if not single:
            sampler.set_phase("finalizing")
        sampler.stopped = True
    if single and sampler_task and not sampler_task.done():
        sampler_task.cancel()
    if sampler_task:
        if single:
            results = await asyncio.gather(sampler_task, return_exceptions=True)
            if any(isinstance(result, Exception) for result in results):
                code, reason = 4, "sampler_failed"
        else:
            try:
                await sampler_task
            except Exception, asyncio.CancelledError:
                code, reason = 4, "sampler_failed"
    if adapter:
        try:
            await adapter.close()
        except Exception:
            code, reason = 4, "adapter_cleanup_failed"
    if single:
        try:
            seal_trial(store, adapter, identity, environment, reason, single=True)
        except (EvidenceError, OSError, ContractError) as exc:
            code, reason = 4, safe_reason(exc, "evidence_io_error")
        try:
            store.close()
        except EvidenceError as exc:
            code, reason = 4, safe_reason(exc, "evidence_io_error")
        if locked:
            lock.__exit__(None, None, None)
    else:
        try:
            seal_trial(
                store,
                adapter,
                identity,
                environment,
                reason,
                safety=safety,
                started_at=started_at,
                allowed_seconds=allowed_seconds,
            )
        except (EvidenceError, OSError, ContractError) as exc:
            code, reason = 4, safe_reason(exc, "evidence_io_error")
        finally:
            try:
                store.close()
            finally:
                if locked:
                    lock.__exit__(None, None, None)
    return code, reason


def seal_trial(
    store,
    adapter,
    identity,
    environment,
    reason,
    *,
    single=False,
    safety=None,
    started_at=None,
    allowed_seconds=None,
):
    if not single or not store.sealed:
        if safety is not None:
            store.snapshot(
                "safety-check.json",
                {
                    "stop_reason": reason if reason != "plan_finished" else None,
                    "last": safety.last,
                },
            )
        if not (store.path / "identity.json").exists():
            store.snapshot("identity.json", identity)
        if not single or not (store.path / "environment.end.json").exists():
            store.snapshot("environment.end.json", environment())
        if not single:
            store.snapshot(
                "execution-budget.json",
                {
                    "schema_version": SCHEMA_VERSION,
                    "run_id": store.run_id,
                    "allocated_seconds": allowed_seconds,
                    "elapsed_seconds": time.monotonic() - started_at,
                    "includes": "preflight_through_cleanup_excludes_final_seal",
                },
            )
        seal_stopped(store, adapter, (reason or "tool_interrupted") if single else reason)
