"""A complete fixed-trial lifecycle on a manually prepared local service."""

import asyncio
import os
import shutil
import time
from dataclasses import asdict, dataclass
from pathlib import Path

from inferyard.analysis.input_lengths import bind_token_counts, check_input_target
from inferyard.analysis.scoring import score_case
from inferyard.config.bundle import require_review
from inferyard.contracts.validation import ContractError
from inferyard.evidence.error_reasons import safe_reason
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.journal import TrialJournal, trial_for
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, Redactor, read_json
from inferyard.evidence.token_budgets import V2, collect_budgets
from inferyard.platforms.identity import PreflightError, memory_available
from inferyard.platforms.resources import ResourceSampler
from inferyard.registry import WINDOWS_BATCH_ADAPTERS, adapter_factory
from inferyard.runtime.request_execution import TrialRequests
from inferyard.runtime.runner import Dependencies, transport_config
from inferyard.runtime.safety import SafetyGuard, monitor
from inferyard.runtime.safety_trace import SafetyTrace
from inferyard.runtime.service_observation import observation_manifest
from inferyard.runtime.signals import install_termination_handler
from inferyard.runtime.trial_finalization import finish_trial
from inferyard.runtime.trial_lifecycle import (
    InitialIdle,
    prepare_service,
    probe_service,
    run_formal,
    warmup_and_baseline,
)
from inferyard.runtime.trial_profile import SingleProfile, TrialDeadline


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
    profile: SingleProfile | None = None,
):
    single = profile is not None
    if (
        not single
        and os.name == "nt"
        and dependencies is None
        and loaded.config.to_dict()["engine"]["adapter"] not in WINDOWS_BATCH_ADAPTERS
    ):
        raise PreflightError("windows_phase2_live_not_supported")
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    deps = dependencies or TrialDependencies(adapter=adapter_factory(config["engine"]["adapter"]))
    if single:
        store = profile.store
        started_at, allowed_seconds, secret = (
            profile.started_at,
            profile.allowed_seconds,
            profile.secret,
        )
    else:
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
    safety = safety_task = safety_reason = safety_trace = None
    restore_signal = install_termination_handler() if single else None
    deadline = TrialDeadline(started_at, allowed_seconds, profile=profile)
    current = deadline.current
    idle_state = InitialIdle() if single else None

    def check_guard(config, files, *, periodic=False):
        config = transport_config(config, adapter)
        if single:
            # P1: single guard keeps its existing identity scope; no disk/memory safety gate.
            deadline.check()
            deps.guard(config, files)
            deadline.check()
            return
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

    budget_task = asyncio.create_task(deadline.wait())
    try:
        if host_lock is None:
            lock.__enter__()
            locked = True
        elif lock.fd is None:
            raise PreflightError("host_lock_not_held")
        from inferyard.config.environment_binding import run_preflight
        from inferyard.runtime.service_reuse import snapshot

        if single:
            snapshot(store, parent, config, lock)
            if profile.kind == "run" and not diagnostic:
                profile.review(loaded.bundle)
        else:
            if service_binding is not None:
                store.snapshot("service-binding.json", service_binding)
            if not diagnostic:
                require_review(loaded.bundle)
            # Journal has verified this frozen workload's repeat/resume parent.
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
            diagnostic=diagnostic or (single and profile.kind != "run"),
            policy=plan["experiment"].get("environment_admission"),
        )
        policy = None if single else plan["experiment"].get("safety")
        if policy is not None:
            safety = deps.safety(policy, config, deps.environment)
            safety_trace = SafetyTrace(store, policy["interval_seconds"])
            store.snapshot("safety-policy.json", safety.metadata())
        if not single:
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
        if single and "device_preflight" in identity:
            store.snapshot("device.preflight.json", identity["device_preflight"])
        adapter = deps.adapter(identity["origin"], secret=secret)
        guard(config, files)
        if single:
            deadline.check()
        else:
            adapter.cache_protocol = store.cache_protocol
        await prepare_service(
            adapter,
            config,
            store,
            lock,
            recovery_confirm=recovery_confirm,
            recovery_note=recovery_note,
            single_kind=profile.kind if single else None,
            idle_state=idle_state,
        )
        by_id = {c["case_id"]: c for c in bundle["cases"]}
        budgets = await collect_budgets(
            adapter,
            config,
            bundle,
            store.selected,
            **({"kind": profile.kind} if single else {}),
        )
        if single:
            deadline.check()
        store.snapshot(V2, budgets)
        workload = None
        if not single:
            workload = next(
                w
                for w in plan["experiment"]["workloads"]
                if w["workload_id"] == trial["workload_id"]
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
        sampler.use_start_environment(identity["environment"])
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
        deadline.execution = execution
        await probe_service(
            execution,
            adapter,
            config,
            store,
            budgets,
            identity,
            output_budget_tokens=workload["output_budget_tokens"] if workload else None,
            check_budget=deadline.check if single else None,
        )
        if single and not (store.path / "identity.json").exists():
            store.snapshot("identity.json", identity)
        if not single or profile.kind == "run":
            await warmup_and_baseline(
                execution, config, sampler, sleep=profile.sleep if single else asyncio.sleep
            )
            await run_formal(
                execution,
                store,
                bundle["cases"] if single else [by_id[cid] for cid in store.selected],
                duration_protocol=None if single else store.duration_protocol,
                capacity_stop=None if single else plan["experiment"].get("capacity_stop"),
            )
            if single:
                deadline.check()
                if diagnostic:
                    code, reason = 3, "diagnostic_run_not_formal"
        else:
            reason = "check_only_no_formal_cases"
    except asyncio.CancelledError:
        code, reason = (
            (3, safety_reason)
            if safety_reason is not None
            else (3, deadline.reason)
            if deadline.expired
            else (130, "user_cancelled")
        )
        if single and idle_state.pending and not (lock.state and lock.state.get("dirty")):
            try:
                lock.dirty(
                    store.run_id, config["endpoint"], "initial_idle_unknown", kind=profile.kind
                )
            except (EvidenceError, OSError) as exc:
                code, reason = 4, safe_reason(exc, "evidence_io_error")
    except UnsupportedFormat:
        raise
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
        code, reason = await finish_trial(
            store,
            adapter,
            sampler,
            sampler_task,
            identity,
            deps.environment,
            code,
            reason,
            lock=lock,
            locked=locked,
            safety=safety,
            started_at=started_at,
            allowed_seconds=allowed_seconds,
            single=single,
        )
        if restore_signal is not None:
            restore_signal()
    if single:
        try:
            summary = read_trial(store.path)["summary"]
        except (EvidenceError, ContractError) as exc:
            code, reason, summary = 4, safe_reason(exc, "evidence_reconstruction_failed"), None
        if code == 0 and execution and execution.unscorable:
            code, reason = 3, "unscorable_case"
        return (
            code,
            {"summary": summary, "reason": reason, "observation": observation_manifest(adapter)},
            store.path,
        )
    readout = getattr(deps, "readout", None)
    metadata = {}
    data = readout(store) if readout is not None else read_trial(store.path, metadata=metadata)
    data["service_drain"] = metadata.get("service_drain")
    if code == 0 and execution and execution.unscorable:
        code = 3
    return code, data, store.path
