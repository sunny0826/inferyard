"""Serial runner for an externally managed service, with durable request boundaries."""

from __future__ import annotations

import asyncio
import os
import time
from copy import deepcopy
from dataclasses import asdict, dataclass
from pathlib import Path
from urllib.parse import urlsplit

from inferyard import __version__
from inferyard.adapters.prism import PrismAdapter
from inferyard.analysis.scoring import ScoringContext, score_case
from inferyard.application.types import CommandResult
from inferyard.config.bundle import require_review
from inferyard.config.loader import LoadedConfig
from inferyard.config.single_plan import compile_single_plan
from inferyard.config.startup_arguments import replace_arguments
from inferyard.contracts.validation import ContractError, Document
from inferyard.evidence.error_reasons import safe_reason
from inferyard.evidence.formats import UnsupportedFormat
from inferyard.evidence.journal import TrialJournal
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import (
    EvidenceError,
    Redactor,
    read_json,
    sha256_file,
    verify_manifest,
)
from inferyard.evidence.token_budgets import V2, collect_budgets, probe_budget
from inferyard.implementation_identity import IdentityContext, execution_matches
from inferyard.platforms.identity import (
    PreflightError,
    environment_snapshot,
    process_start_ticks,
    resolve_loopback_origin,
    static_preflight,
    verify_process,
)
from inferyard.platforms.telemetry import Sampler
from inferyard.provenance import tool_source_hash
from inferyard.registry import adapter_collector_id, adapter_factory, collector_factory
from inferyard.runtime.lock import HostLock
from inferyard.runtime.request_execution import TrialRequests
from inferyard.runtime.service_observation import (
    clean_observed,
    observation_manifest,
    observe_service,
)
from inferyard.runtime.signals import install_termination_handler


def _guard(config, files):
    if not all(item.unchanged() for item in files):
        raise PreflightError("identity_file_changed")
    _, address, port = resolve_loopback_origin(config["endpoint"]["url"])
    verify_process(
        config,
        files[0],
        files[1],
        address,
        port,
    )


@dataclass
class Dependencies:
    preflight: object = static_preflight
    adapter: object = PrismAdapter
    lock: object = HostLock
    sampler: object = Sampler
    guard: object = _guard
    scorer: object = score_case
    environment: object = environment_snapshot


def load_rerun(request, scoring_context=None, identity_context=None) -> tuple[LoadedConfig, dict]:
    scoring_context = scoring_context or ScoringContext()
    identity_context = identity_context or IdentityContext(source_hash=tool_source_hash)
    root = request.from_run
    limitations = verify_manifest(root)
    if any("manifest_missing" in reason for reason in limitations):
        raise PreflightError("rerun_requires_sealed_source")
    metadata = {}
    parent = read_trial(root, metadata=metadata)
    parent["service_drain"] = metadata.get("service_drain")
    config, bundle, meta = (deepcopy(parent[k]) for k in ("config", "bundle", "run"))
    if meta["execution_mode"] != "single" or meta["kind"] != "run":
        raise PreflightError("rerun_requires_single_run_source")
    if (
        parent["selection"]["scorer_sha256"] != scoring_context.identity()
        or meta["tool_version"] != __version__
    ):
        raise PreflightError("rerun_tool_or_scorer_changed")
    if "implementation_identity" in meta:
        if not execution_matches(meta["implementation_identity"], identity_context.value):
            raise PreflightError("rerun_tool_source_changed_or_unknown")
    elif meta.get("tool_source_sha256") != identity_context.source:
        raise PreflightError("rerun_tool_source_changed_or_unknown")
    new_start = process_start_ticks(request.server_pid)
    old_url = urlsplit(config["endpoint"]["url"])
    new_url = urlsplit(request.endpoint_url)
    config["engine"]["startup_args"] = replace_arguments(
        config["engine"]["startup_args"],
        {
            "--host": new_url.hostname,
            "--port": str(new_url.port or (443 if new_url.scheme == "https" else 80)),
        },
        expected_values={"--host": old_url.hostname},
    )
    config["endpoint"].update(
        url=request.endpoint_url, server_pid=request.server_pid, process_start_ticks=new_start
    )
    from inferyard.runtime.service_reuse import require_transition

    require_transition(parent, config, process_start_ticks, reason="rerun_requires_new_service")
    if getattr(request, "output_root", None):
        config["output"]["root"] = str(request.output_root.resolve())
    if getattr(request, "api_key_env", None):
        config["endpoint"]["api_key_env"] = request.api_key_env
    source = root / "config.input.toml"
    if not source.exists():
        source = root / "config.frozen.json"
    return LoadedConfig(
        source,
        Document.parse("config", config),
        Document.parse("bundle", bundle),
        sha256_file(source),
        sha256_file(root / "bundle.json"),
        (),
    ), parent


async def execute_async(request, dependencies: Dependencies | None = None):
    scoring_context = ScoringContext()
    identity_context = IdentityContext(source_hash=tool_source_hash)
    loaded, parent = (
        load_rerun(request, scoring_context, identity_context)
        if request.from_run
        else (request.config, None)
    )
    config, bundle = loaded.config.to_dict(), loaded.bundle.to_dict()
    deps = dependencies or Dependencies(
        adapter=adapter_factory(config["engine"]["adapter"]),
        sampler=collector_factory(adapter_collector_id(config["engine"]["adapter"])),
    )
    key_name = config["endpoint"].get("api_key_env")
    secret = os.environ.get(key_name) if key_name else None
    if key_name and not secret:
        raise PreflightError("credential_reference_unavailable")
    redactor = Redactor([secret] if secret else [])
    plan = compile_single_plan(config, bundle)
    started_at = time.monotonic()
    allowed_seconds = plan["experiment"]["budget"]["max_wall_seconds"]
    store = TrialJournal(
        Path(config["output"]["root"]),
        plan,
        plan["trials"][0]["trial_id"],
        config,
        bundle,
        redactor=redactor,
        rerun_parent=parent,
        diagnostic=request.diagnostic,
        kind=request.command,
        execution_mode="single",
        scoring_context=scoring_context,
        source_identity=identity_context.source,
        implementation_identity=identity_context.value,
    )
    source_name = "config.input.toml" if loaded.source.suffix == ".toml" else "config.input.json"
    store.text_snapshot(source_name, loaded.source.read_text())
    store.snapshot(
        "input-provenance.json",
        {
            "input_config_sha256": loaded.input_sha256,
            "input_bundle_sha256": loaded.bundle_sha256,
            "defaulted_fields": list(loaded.defaulted_fields),
        },
    )
    if parent:
        store.snapshot(
            "parent.json",
            {
                "run_id": parent["run"]["run_id"],
                "path": str(request.from_run),
                "manifest_sha256": sha256_file(request.from_run / "manifest.json"),
                "events_sha256": parent["events_sha256"],
            },
        )
    adapter = None
    sampler = None
    sampler_task = None
    execution = None
    reason = "plan_finished"
    code = 0
    identity = {}
    lock, locked = deps.lock(), False
    restore_signal = install_termination_handler()
    current = asyncio.current_task()
    prior_cancellations = current.cancelling()
    budget_expired = initial_idle_pending = False

    def expire_budget():
        nonlocal budget_expired
        budget_expired = True
        if execution is not None:
            execution.cancellation_reason = "single_wall_budget_exhausted"

    def check_budget():
        if time.monotonic() - started_at >= allowed_seconds:
            expire_budget()
            raise asyncio.CancelledError

    async def deadline():
        await asyncio.sleep(max(0, allowed_seconds - (time.monotonic() - started_at)))
        # A user cancellation already draining must retain its original reason.
        if current.cancelling() == prior_cancellations:
            expire_budget()
            current.cancel()

    def guard(config, files):
        check_budget()
        deps.guard(config, files)
        check_budget()

    budget_task = asyncio.create_task(deadline())
    try:
        lock.__enter__()
        locked = True
        from inferyard.runtime.service_reuse import snapshot

        snapshot(store, parent, config, lock)
        if request.command == "run" and not request.diagnostic:
            require_review(bundle)
        from inferyard.config.environment_binding import run_preflight

        identity, files = run_preflight(
            deps.preflight,
            config,
            diagnostic=request.diagnostic or request.command != "run",
            policy=plan["experiment"].get("environment_admission"),
        )
        identity["files"] = [asdict(file) for file in files]
        identity["runtime_library_hashes"] = read_json(
            Path(config["engine"]["runtime_library_manifest"])
        )
        store.snapshot("environment.start.json", identity["environment"])
        if "device_preflight" in identity:
            store.snapshot("device.preflight.json", identity["device_preflight"])
        adapter = deps.adapter(identity["origin"], secret=secret)
        check_budget()

        async def wait_idle(phase="probe", request_id=None):
            return await observe_service(
                adapter, config["execution"]["idle_wait_seconds"], store, phase, request_id
            )

        pre_idle_props = None
        if config["engine"]["adapter"] in ("kvmem", "ninfer"):
            pre_idle_props = await adapter.verify_properties(config)
            store.snapshot("service.props.json", pre_idle_props)
            store.snapshot("engine-capabilities.json", adapter.capability_evidence)
        recovery = None
        if request.recovery_confirm:
            recovery = lock.verify_manual_recovery(
                request.recovery_confirm, request.recovery_note, config["endpoint"]
            )
        old_state = lock.state
        if old_state and old_state.get("dirty") and not recovery:
            if (old_state.get("server_pid"), old_state.get("process_start_ticks")) != (
                config["endpoint"]["server_pid"],
                config["endpoint"]["process_start_ticks"],
            ):
                raise PreflightError("dirty_service_requires_bound_recovery")
        initial_idle_pending = True
        if not await wait_idle():
            if not old_state or not old_state.get("dirty"):
                lock.dirty(
                    store.run_id,
                    config["endpoint"],
                    "initial_idle_unknown",
                    kind=request.command,
                )
            raise PreflightError("initial_service_not_idle")
        clean_observed(lock, adapter, store, recovery=recovery)
        initial_idle_pending = False
        if recovery:
            store.snapshot("recovery.json", recovery)
        props = pre_idle_props or await adapter.verify_properties(config)
        if pre_idle_props is None:
            store.snapshot("service.props.json", props)
        budgets = await collect_budgets(
            adapter, config, bundle, store.selected, kind=request.command
        )
        check_budget()
        store.snapshot(V2, budgets)
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
        one = execution.one

        for stream in (False, True):
            response = await one("probe", config["execution"]["probe_prompt"], stream=stream)
            if response["execution_state"] != "completed":
                raise PreflightError("protocol_probe_failed")
            effective = await adapter.verify_effective(
                config, probe_budget(budgets)["input_tokens"], adapter.last_state
            )
            store.snapshot(
                "effective-stream.json" if stream else "effective-ordinary.json", effective
            )
            identity["effective_parameters"] = effective
            check_budget()
        store.snapshot("identity.json", identity)
        if request.command == "run":
            for _ in range(config["execution"]["warmup_count"]):
                response = await one("warmup", config["execution"]["warmup_prompt"])
                if response["execution_state"] != "completed":
                    raise PreflightError("warmup_failed")
            sampler.set_phase("baseline")
            await asyncio.sleep(config["telemetry"]["baseline_seconds"])
            for index, case in enumerate(bundle["cases"]):
                await one("formal", case["prompt"], index=index)
            check_budget()
            if request.diagnostic:
                reason = "diagnostic_run_not_formal"
                code = 3
        else:
            reason = "check_only_no_formal_cases"
    except asyncio.CancelledError:
        code, reason = (
            (3, "single_wall_budget_exhausted") if budget_expired else (130, "user_cancelled")
        )
        if initial_idle_pending and not (lock.state and lock.state.get("dirty")):
            try:
                lock.dirty(
                    store.run_id, config["endpoint"], "initial_idle_unknown", kind=request.command
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
        await asyncio.gather(budget_task, return_exceptions=True)
        if sampler:
            sampler.stopped = True
        if sampler_task and not sampler_task.done():
            sampler_task.cancel()
        if sampler_task:
            results = await asyncio.gather(sampler_task, return_exceptions=True)
            if any(isinstance(result, Exception) for result in results):
                code, reason = 4, "sampler_failed"
        if adapter:
            try:
                await adapter.close()
            except Exception:
                code, reason = 4, "adapter_cleanup_failed"
        if not store.sealed:
            try:
                if not (store.path / "identity.json").exists():
                    store.snapshot("identity.json", identity)
                if not (store.path / "environment.end.json").exists():
                    store.snapshot("environment.end.json", deps.environment())
                store.event(
                    "run_stopped", "finalizing", None, {"reason": reason or "tool_interrupted"}
                )
                if extra := observation_manifest(adapter):
                    store.seal(extra)
                else:
                    store.seal()
            except (EvidenceError, OSError, ContractError) as exc:
                code, reason = 4, safe_reason(exc, "evidence_io_error")
        try:
            store.close()
        except EvidenceError as exc:
            code, reason = 4, safe_reason(exc, "evidence_io_error")
        if locked:
            lock.__exit__(None, None, None)
        restore_signal()
    try:
        summary = read_trial(store.path)["summary"]
    except (EvidenceError, ContractError) as exc:
        code, reason, summary = 4, safe_reason(exc, "evidence_reconstruction_failed"), None
    if code == 0 and execution and execution.unscorable:
        code, reason = 3, "unscorable_case"
    return code, CommandResult(
        request.command,
        "finished"
        if code == 0
        else "interrupted"
        if code == 130
        else "blocked"
        if code == 2
        else "error",
        summary["completeness"] if summary else "incomplete",
        store.run_id,
        str(store.path),
        tuple(
            ([reason] if reason != "plan_finished" else [])
            + (summary["limitations"] if summary else [])
        ),
        details={
            **observation_manifest(adapter),
            "evidence_only": True,
            "next_report_command": [
                "inferyard",
                "report",
                "--runs",
                str(store.path),
                "--out",
                str(store.path.parent / (store.run_id + "-report")),
            ],
        },
    )


def execute(request):
    return asyncio.run(execute_async(request))
