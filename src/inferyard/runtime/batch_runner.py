"""Serial fixed-plan controller; service changes remain explicit operator actions."""

import asyncio
import os
import sys
from dataclasses import replace

from inferyard.analysis.repetition_metrics import repetition_metrics
from inferyard.analysis.scoring import ScoringContext
from inferyard.application.types import CommandResult
from inferyard.contracts.validation import Document
from inferyard.evidence.ledger import plan_progress, resume_selection
from inferyard.evidence.storage import EvidenceError, read_json
from inferyard.implementation_identity import IdentityContext
from inferyard.platforms.identity import PreflightError
from inferyard.provenance import tool_source_hash
from inferyard.registry import (
    WINDOWS_BATCH_ADAPTERS,
    adapter_collector_id,
    adapter_factory,
    collector_factory,
    registered_score,
    require_execution_support,
)
from inferyard.runtime.batch_state import (
    bind_service,
    history,
    open_batch,
    remaining_budget,
    require_replaced_service,
)
from inferyard.runtime.trial_runner import TrialDependencies, run_trial


def _result(request, root, plan, runs, status, code=0, next_trial=None):
    progress = plan_progress(plan, runs)
    complete = all(t["status"] == "complete" for t in progress)
    latest = runs[-1] if runs else None
    if code == 0 and any(
        (r.get("score") or {}).get("quality_state") == "unscorable"
        for d in runs
        for r in d["requests"]
    ):
        code = 3
    details = {
        "plan_sha256": plan["plan_sha256"],
        "trials": progress,
        "repetition_metrics": repetition_metrics(plan, runs),
        "remaining_wall_budget_seconds": remaining_budget(plan, runs),
        "next_trial": next_trial,
        "runs": [str(d["path"]) for d in runs],
        "last_stop_reason": latest["summary"]["stop_reason"] if latest else None,
    }
    return code, CommandResult(
        request.command,
        status,
        "complete" if complete else "incomplete",
        latest["run"]["run_id"] if latest else None,
        str(root),
        () if complete else ("experiment_incomplete",),
        details,
    )


def _next(plan, runs):
    for trial in plan["trials"]:
        attempts = [d for d in runs if d["run"]["trial_id"] == trial["trial_id"]]
        if not attempts:
            return trial, None
        latest = attempts[-1]
        if (
            "duration" in latest["summary"]
            and not latest["summary"]["duration"]["window_completed"]
        ):
            return trial, latest
        if any(r["execution_state"] == "not_executed" for r in latest["requests"]):
            return trial, latest
    return None, None


async def execute_async(request, dependencies=None):
    deps = dependencies or TrialDependencies(scorer=registered_score)
    scoring_context = ScoringContext()
    identity_context = IdentityContext(source_hash=tool_source_hash)
    source_identity = identity_context.source
    resume = request.command == "resume"
    root = request.from_run.parent.parent if resume else request.output_root
    if root is None:
        raise PreflightError("batch_output_root_required")
    root = root.resolve()
    with deps.lock() as lock:
        diagnostic = read_json(root / "batch.json")["diagnostic"] if resume else request.diagnostic
        plan, loaded = open_batch(
            root,
            None if resume else request.frozen_plan,
            diagnostic,
            source_identity=source_identity,
            implementation_identity=identity_context.value,
        )
        require_execution_support(plan, loaded)
        if (
            os.name == "nt"
            and dependencies is None
            and any(
                item.config.to_dict()["engine"]["adapter"] not in WINDOWS_BATCH_ADAPTERS
                for item in loaded.values()
            )
        ):
            raise PreflightError("windows_phase2_live_not_supported")
        runs = history(
            root,
            plan,
            loaded,
            source_identity=source_identity,
            implementation_identity=identity_context.value,
        )
        if plan["experiment"].get("capacity_stop") == "first_failed_request":
            stopped = next(
                (
                    d
                    for d in runs
                    if d["summary"]["stop_reason"] != "plan_finished"
                    or any(r["execution_state"] != "completed" for r in d["requests"])
                ),
                None,
            )
            if stopped is not None:
                return _result(request, root, plan, runs, "capacity_scan_stopped", 3)
        pending, interrupted = _next(plan, runs)
        if interrupted and "duration" in interrupted["summary"]:
            return _result(request, root, plan, runs, "duration_requires_new_window", 3, pending)
        if resume:
            source = next(
                (d for d in runs if d["path"].resolve() == request.from_run.resolve()), None
            )
            if source is None or request.from_run.parent.resolve() != (root / "runs").resolve():
                raise EvidenceError("resume_source_not_in_batch")
            if (
                pending is None
                or interrupted is None
                or source["run"]["run_id"] != interrupted["run"]["run_id"]
            ):
                raise PreflightError("resume_requires_latest_unfinished_run")
            selection = resume_selection(source)
        else:
            source, selection = None, ()
            if pending is None:
                return _result(request, root, plan, runs, "finished")
            if interrupted is not None:
                return _result(
                    request,
                    root,
                    plan,
                    runs,
                    "resume_required",
                    3,
                    {"trial_id": pending["trial_id"], "from_run": str(interrupted["path"])},
                )
        workload_id = pending["workload_id"]
        if request.workload_id is not None and request.workload_id != workload_id:
            raise PreflightError("workload_is_not_next_in_frozen_plan")
        base = loaded[workload_id]
        previous = runs[-1] if runs else None
        same_workload = previous is not None and any(
            t["trial_id"] == previous["run"]["trial_id"] and t["workload_id"] == workload_id
            for t in plan["trials"]
        )
        if source is not None or same_workload:
            # Resume keeps the most recently verified service binding, while corpus relocation
            # uses the current package. Immutable fields were checked against the frozen plan.
            prior = source or previous
            config = prior["config"].copy()
            config["bundle"] = base.config.to_dict()["bundle"]
            base = replace(base, config=Document.parse("config", config))
        bound, binding = bind_service(
            base, request.endpoint_url, request.server_pid, request.api_key_env
        )
        fresh_process = bound.config.to_dict()["execution"].get("require_fresh_process", False)
        if previous is not None and (not same_workload or fresh_process):
            if not request.workload_id or not request.handoff_note or request.server_pid is None:
                return _result(
                    request,
                    root,
                    plan,
                    runs,
                    "awaiting_handoff",
                    3,
                    {
                        "trial_id": pending["trial_id"],
                        "workload_id": workload_id,
                        "required": [
                            "--workload",
                            "--endpoint-url",
                            "--server-pid",
                            "--handoff-note",
                        ],
                    },
                )
        if previous is not None:
            binding["transition"] = require_replaced_service(
                previous, bound.config.to_dict(), serial_continuation=same_workload
            )
            binding["cache_state"] = "unknown"
            binding["warmup_count"] = bound.config.to_dict()["execution"]["warmup_count"]
            binding["previous_drain"] = previous.get("service_drain")
        binding.update(
            workload_id=workload_id,
            note=request.handoff_note,
            previous_run_id=previous["run"]["run_id"] if previous else None,
        )
        if dependencies is None:
            deps.adapter = adapter_factory(bound.config.to_dict()["engine"]["adapter"])
            deps.sampler = collector_factory(
                adapter_collector_id(bound.config.to_dict()["engine"]["adapter"], batch=True)
            )
        selected_trials = (
            [pending]
            if resume
            else [
                t
                for t in plan["trials"]
                if t["workload_id"] == workload_id and t["repeat_index"] >= pending["repeat_index"]
            ]
        )
        if fresh_process:
            selected_trials = selected_trials[:1]
        for trial in selected_trials:
            wall = remaining_budget(plan, runs)
            if wall <= 0:
                return _result(request, root, plan, runs, "budget_exhausted", 3, trial)
            parent = (
                source
                if resume
                else next(
                    (
                        d
                        for d in reversed(runs)
                        if any(
                            t["trial_id"] == d["run"]["trial_id"]
                            and t["workload_id"] == workload_id
                            for t in plan["trials"]
                        )
                    ),
                    None,
                )
            )
            print(f"trial {trial['repeat_index'] + 1}: {trial['trial_id']}", file=sys.stderr)
            code, data, path = await run_trial(
                plan,
                trial["trial_id"],
                bound,
                root / "runs",
                parent=parent,
                resume_case_ids=selection,
                diagnostic=diagnostic,
                recovery_confirm=request.recovery_confirm,
                recovery_note=request.recovery_note,
                dependencies=deps,
                host_lock=lock,
                service_binding=binding,
                wall_budget_seconds=wall,
                scoring_context=scoring_context,
                source_identity=source_identity,
                implementation_identity=identity_context.value,
            )
            data["path"] = path
            runs.append(data)
            # Recovery tokens are one-time and bound to a dirty marker, not to all repetitions.
            scoring_partial = (
                code == 3
                and data["summary"]["scope_complete"]
                and data["summary"]["stop_reason"] == "plan_finished"
                and any(r.get("quality_state") == "unscorable" for r in data["requests"])
            )
            if code and not scoring_partial:
                return _result(
                    request,
                    root,
                    plan,
                    runs,
                    "interrupted" if code == 130 else "blocked" if code == 2 else "error",
                    code,
                )
            request = replace(request, recovery_confirm=None, recovery_note=None)
        pending, interrupted = _next(plan, runs)
        if pending is None:
            status = "finished"
        elif resume:
            status = "resumed_subset"
        else:
            status = "awaiting_handoff"
        return _result(
            request, root, plan, runs, status, 0 if pending is None or resume else 3, pending
        )


def execute(request):
    async def dispatch():
        from inferyard.runtime.signals import install_termination_handler

        restore_signal = install_termination_handler()
        try:
            return await execute_async(request)
        finally:
            restore_signal()

    return asyncio.run(dispatch())
