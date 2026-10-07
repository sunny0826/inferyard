"""A complete fixed-trial lifecycle on a manually prepared local service."""

import asyncio
import os
import shutil
import time
from dataclasses import asdict, dataclass
from pathlib import Path

from inferyard import SCHEMA_VERSION
from inferyard.analysis.input_lengths import bind_token_counts, check_input_target
from inferyard.analysis.scoring import score_case
from inferyard.config.bundle import require_review
from inferyard.contracts.validation import ContractError
from inferyard.evidence.error_reasons import safe_reason
from inferyard.evidence.journal import TrialJournal, trial_for
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, Redactor, read_json
from inferyard.evidence.token_budgets import V2, collect_budgets, probe_budget
from inferyard.platforms.identity import PreflightError, memory_available
from inferyard.platforms.resources import ResourceSampler
from inferyard.registry import WINDOWS_BATCH_ADAPTERS, adapter_factory
from inferyard.runtime.duration_driver import run_duration
from inferyard.runtime.request_execution import TrialRequests
from inferyard.runtime.runner import Dependencies
from inferyard.runtime.safety import SafetyGuard, monitor
from inferyard.runtime.safety_trace import SafetyTrace
from inferyard.runtime.service_observation import (
    clean_observed,
    observation_manifest,
    observe_service,
)


@dataclass
class TrialDependencies(Dependencies):
    scorer: object = score_case
    sampler: object = ResourceSampler
    safety: object = SafetyGuard
    journal: object = TrialJournal
    readout: object = None


async def run_trial(
    plan,
    trial_id,
    loaded,
    output_root,
    *,
    parent=None,
    resume_case_ids=(),
    diagnostic=False,
    recovery_confirm=None,
    recovery_note=None,
    dependencies=None,
    host_lock=None,
    service_binding=None,
    wall_budget_seconds=None,
    scoring_context=None,
    source_identity=None,
    implementation_identity=None,
):
    if (
        os.name == "nt"
        and dependencies is None
        and loaded.config.to_dict()["engine"]["adapter"] not in WINDOWS_BATCH_ADAPTERS
    ):
        raise PreflightError("windows_phase2_live_not_supported")
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    deps = dependencies or TrialDependencies(adapter=adapter_factory(config["engine"]["adapter"]))
    trial = trial_for(plan, trial_id)
    allowed_seconds = (
        min(trial["total_budget_seconds"], wall_budget_seconds)
        if wall_budget_seconds is not None
        else trial["total_budget_seconds"]
    )
    if allowed_seconds <= 0:
        raise PreflightError("experiment_wall_budget_exhausted")
    started_at = time.monotonic()
    secret_name = config["endpoint"].get("api_key_env")
    secret = os.environ.get(secret_name) if secret_name else None
    if secret_name and not secret:
        raise PreflightError("credential_reference_unavailable")
    store = getattr(deps, "journal", TrialJournal)(
        output_root,
        plan,
        trial_id,
        config,
        bundle,
        redactor=Redactor([secret] if secret else []),
        parent=parent,
        resume_case_ids=resume_case_ids,
        diagnostic=diagnostic,
        scoring_context=scoring_context,
        source_identity=source_identity,
        implementation_identity=implementation_identity,
    )
    lock, locked = host_lock or deps.lock(), False
    adapter = sampler = sampler_task = execution = None
    identity, reason, code = {}, "plan_finished", 0
    budget_expired = False
    safety = safety_task = safety_reason = safety_trace = None
    current = asyncio.current_task()

    async def deadline():
        nonlocal budget_expired
        remaining = max(0, allowed_seconds - (time.monotonic() - started_at))
        await asyncio.sleep(remaining)
        budget_expired = True
        if execution is not None:
            execution.cancellation_reason = "trial_wall_budget_exhausted"
        current.cancel()

    def check_guard(config, files, *, periodic=False):
        budget = plan["experiment"]["budget"]
        if shutil.disk_usage(store.path).free < budget["min_disk_bytes"]:
            raise PreflightError("disk_safety_budget_reached")
        if memory_available() < budget["min_available_memory_bytes"]:
            raise PreflightError("memory_safety_budget_reached")
        deps.guard(config, files)
        if safety is not None:
            safety.check(periodic=periodic)

    def guard(config, files, *, periodic=False):
        if safety_trace is None:
            return check_guard(config, files, periodic=periodic)
        return safety_trace.check(
            lambda: check_guard(config, files, periodic=periodic), safety, periodic=periodic
        )

    def safety_stop(value):
        nonlocal safety_reason
        safety_reason = value
        if execution is not None:
            execution.cancellation_reason = value
        current.cancel()

    budget_task = asyncio.create_task(deadline())
    try:
        if host_lock is None:
            lock.__enter__()
            locked = True
        elif lock.fd is None:
            raise PreflightError("host_lock_not_held")
        if service_binding is not None:
            store.snapshot("service-binding.json", service_binding)
        if not diagnostic:
            require_review(bundle)
        from inferyard.config.environment_binding import run_preflight
        from inferyard.runtime.service_reuse import snapshot

        # TrialJournal has verified parent as this frozen workload's repeat/resume.
        # Preserve its existing idle/native client completion and recovery path below.
        snapshot(store, parent, config, lock, serial_continuation=True)
        if (
            service_binding
            and parent is None
            and service_binding.get("transition") == "same_process"
            and lock.state
            and lock.state.get("dirty")
        ):
            raise PreflightError("dirty_service_requires_bound_recovery")
        identity, files = run_preflight(
            deps.preflight,
            config,
            diagnostic=diagnostic,
            policy=plan["experiment"].get("environment_admission"),
        )
        policy = plan["experiment"].get("safety")
        if policy is not None:
            safety = deps.safety(policy, config, deps.environment)
            safety_trace = SafetyTrace(store, policy["interval_seconds"])
            store.snapshot("safety-policy.json", safety.metadata())
        guard(config, files)
        if policy is not None:
            safety_task = asyncio.create_task(
                monitor(
                    policy["interval_seconds"],
                    lambda: guard(config, files, periodic=True),
                    safety_stop,
                )
            )
        identity["files"] = [asdict(f) for f in files]
        identity["runtime_library_hashes"] = read_json(
            Path(config["engine"]["runtime_library_manifest"])
        )
        store.snapshot("environment.start.json", identity["environment"])
        adapter = deps.adapter(identity["origin"], secret=secret)
        adapter.cache_protocol = store.cache_protocol
        pre_idle_props = None
        if config["engine"]["adapter"] in ("kvmem", "ninfer"):
            pre_idle_props = await adapter.verify_properties(config)
            store.snapshot("service.props.json", pre_idle_props)
            store.snapshot("engine-capabilities.json", adapter.capability_evidence)
        recovery = None
        if recovery_confirm:
            recovery = lock.verify_manual_recovery(
                recovery_confirm, recovery_note, config["endpoint"]
            )
        if lock.state and lock.state.get("dirty") and not recovery:
            if (lock.state.get("server_pid"), lock.state.get("process_start_ticks")) != (
                config["endpoint"]["server_pid"],
                config["endpoint"]["process_start_ticks"],
            ):
                raise PreflightError("dirty_service_requires_bound_recovery")
        idle = await observe_service(adapter, config["execution"]["idle_wait_seconds"], store)
        if not idle:
            if not lock.state or not lock.state.get("dirty"):
                lock.dirty(store.run_id, config["endpoint"], "initial_idle_unknown")
            raise PreflightError("initial_service_not_idle")
        clean_observed(lock, adapter, store, recovery=recovery)
        if recovery:
            store.snapshot("recovery.json", recovery)
        if pre_idle_props is None:
            store.snapshot("service.props.json", await adapter.verify_properties(config))
        by_id = {c["case_id"]: c for c in bundle["cases"]}
        budgets = await collect_budgets(adapter, config, bundle, store.selected)
        store.snapshot(V2, budgets)
        workload = next(
            w for w in plan["experiment"]["workloads"] if w["workload_id"] == trial["workload_id"]
        )
        counts = bind_token_counts(
            store.selected,
            budgets,
            workload["output_budget_tokens"],
            source="/lab/v1/token-budget"
            if config["engine"]["adapter"] in ("kvmem", "ninfer")
            else "apply-template+tokenize:add_special,parse_special",
        )
        target_check = check_input_target(workload, store.selected, counts)
        store.snapshot("input-target-check.json", target_check)
        if target_check["status"] not in ("not_requested", "matched"):
            raise PreflightError("input_length_target_" + target_check["status"])
        sampler = deps.sampler(store, config)
        sampler_task = asyncio.create_task(sampler.run())
        execution = TrialRequests(
            store,
            config,
            bundle,
            adapter,
            sampler,
            sampler_task,
            lock,
            guard,
            files,
            scorer=deps.scorer,
        )
        for stream in (False, True):
            response = await execution.one(
                "probe", config["execution"]["probe_prompt"], stream=stream
            )
            if response["execution_state"] != "completed":
                raise PreflightError("protocol_probe_failed")
            effective = await adapter.verify_effective(
                config, probe_budget(budgets)["input_tokens"], adapter.last_state
            )
            if store.strict_output:
                from inferyard.runtime.fixed_output import verify_probe

                effective["strict_output"] = verify_probe(
                    effective, response, workload["output_budget_tokens"]
                )
            store.snapshot(
                "effective-stream.json" if stream else "effective-ordinary.json", effective
            )
            identity["effective_parameters"] = effective
        for _ in range(config["execution"]["warmup_count"]):
            response = await execution.one("warmup", config["execution"]["warmup_prompt"])
            if response["execution_state"] != "completed":
                raise PreflightError("warmup_failed")
        sampler.set_phase("baseline")
        await asyncio.sleep(config["telemetry"]["baseline_seconds"])
        if store.duration_protocol:

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

            window = await run_duration(store.duration_protocol, store.selected, request, emit)
            if not window["window_completed"]:
                raise PreflightError(window["reason"])
        else:
            for index, cid in enumerate(store.selected):
                terminal = await execution.one("formal", by_id[cid]["prompt"], index=index)
                if (
                    plan["experiment"].get("capacity_stop") == "first_failed_request"
                    and terminal["execution_state"] == "failed"
                ):
                    raise PreflightError("capacity_scan_failed_request")
    except asyncio.CancelledError:
        code, reason = (
            (3, safety_reason)
            if safety_reason is not None
            else (3, "trial_wall_budget_exhausted")
            if budget_expired
            else (130, "user_cancelled")
        )
    except (PreflightError, ContractError) as exc:
        code = 3 if execution and execution.formal_started else 2
        reason = exc.reason if isinstance(exc, ContractError) else str(exc)
    except (EvidenceError, OSError) as exc:
        code, reason = 4, safe_reason(exc, "evidence_io_error")
    except Exception:
        code, reason = 4, "tool_internal_error"
    finally:
        budget_task.cancel()
        if safety_task is not None:
            safety_task.cancel()
        await asyncio.gather(
            budget_task, *([safety_task] if safety_task else []), return_exceptions=True
        )
        if sampler:
            sampler.set_phase("finalizing")
            sampler.stopped = True
        if sampler_task:
            try:
                await sampler_task
            except Exception, asyncio.CancelledError:
                code, reason = 4, "sampler_failed"
        if adapter:
            try:
                await adapter.close()
            except Exception:
                code, reason = 4, "adapter_cleanup_failed"
        try:
            if safety is not None:
                store.snapshot(
                    "safety-check.json",
                    {
                        "stop_reason": reason if reason != "plan_finished" else None,
                        "last": safety.last,
                    },
                )
            store.snapshot("identity.json", identity)
            store.snapshot("environment.end.json", deps.environment())
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
            store.event("run_stopped", "finalizing", None, {"reason": reason})
            if extra := observation_manifest(adapter):
                store.seal(extra)
            else:
                store.seal()
        except (EvidenceError, OSError, ContractError) as exc:
            code, reason = 4, safe_reason(exc, "evidence_io_error")
        finally:
            try:
                store.close()
            finally:
                if locked:
                    lock.__exit__(None, None, None)
    readout = getattr(deps, "readout", None)
    metadata = {}
    data = readout(store) if readout is not None else read_trial(store.path, metadata=metadata)
    data["service_drain"] = metadata.get("service_drain")
    if code == 0 and execution and execution.unscorable:
        code = 3
    return code, data, store.path
