"""Command results for execution commands; execution backends load only when dispatched."""

import asyncio

_RUN_STATUS = {0: "finished", 130: "interrupted", 2: "blocked"}
# Domain batch states map one-to-one to the CLI status vocabulary.
_BATCH_STATUS = {
    "finished": "finished",
    "resumed_subset": "resumed_subset",
    "awaiting_handoff": "awaiting_handoff",
    "resume_required": "resume_required",
    "duration_requires_new_window": "duration_requires_new_window",
    "budget_exhausted": "budget_exhausted",
    "capacity_scan_stopped": "capacity_scan_stopped",
    "comparison_history_stopped": "comparison_history_stopped",
    "interrupted": "interrupted",
    "blocked": "blocked",
    "error": "error",
}


def _run_result(request, outcome):
    from inferyard.application.types import CommandResult

    summary = outcome.summary
    return CommandResult(
        request.command,
        _RUN_STATUS.get(outcome.code, "error"),
        summary["completeness"] if summary else "incomplete",
        outcome.run_id,
        str(outcome.path),
        tuple(
            ([outcome.reason] if outcome.reason != "plan_finished" else [])
            + (summary["limitations"] if summary else [])
        ),
        details={
            **outcome.observation,
            "evidence_only": True,
            "next_report_command": [
                "inferyard",
                "report",
                "--runs",
                str(outcome.path),
                "--out",
                str(outcome.path.parent / (outcome.run_id + "-report")),
            ],
        },
    )


def _batch_result(request, outcome):
    from inferyard.analysis.repetition_metrics import repetition_metrics
    from inferyard.application.types import CommandResult
    from inferyard.analysis.ledger import plan_progress
    from inferyard.runtime.batch_state import remaining_budget

    plan, runs = outcome.plan, outcome.runs
    code = outcome.code
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
        **(
            {"next_results_command": ["inferyard", "speed-results", "--batch", str(outcome.root)]}
            if plan["experiment"].get("profile") == "context-speed.v1"
            else {"repetition_metrics": repetition_metrics(plan, runs)}
        ),
        "remaining_wall_budget_seconds": remaining_budget(plan, runs),
        "next_trial": outcome.next_trial,
        "runs": [str(d["path"]) for d in runs],
        "last_stop_reason": latest["summary"]["stop_reason"] if latest else None,
    }
    if outcome.comparison_stop is not None:
        details["comparison_stop"] = outcome.comparison_stop
    return code, CommandResult(
        request.command,
        _BATCH_STATUS[outcome.state],
        "complete" if complete else "incomplete",
        latest["run"]["run_id"] if latest else None,
        str(outcome.root),
        () if complete else ("experiment_incomplete",),
        details,
    )


def _is_batch(request):
    return request.command == "resume" or (
        request.command == "run" and request.frozen_plan is not None
    )


async def execute_async(request, dependencies=None):
    if _is_batch(request):
        from inferyard.runtime.batch_runner import execute_async as run

        return _batch_result(request, await run(request, dependencies))
    from inferyard.runtime.runner import execute_async as run

    code, outcome = await run(request, dependencies)
    return code, _run_result(request, outcome)


def _prepare_length(request):
    from inferyard.application.types import CommandResult
    from inferyard.runtime import length_prepare

    result, prompt_present = asyncio.run(length_prepare.prepare(request))
    return (
        0 if result["status"] == "matched" else 3,
        CommandResult(
            request.command,
            result["status"],
            "complete" if prompt_present else "incomplete",
            evidence_dir=str(request.out),
            limitations=tuple(result["limitations"]),
            details=result,
        ),
    )


def execute(request):
    if request.command == "prepare-length":
        return _prepare_length(request)
    if _is_batch(request):

        async def dispatch():
            from inferyard.runtime.signals import install_termination_handler

            restore_signal = install_termination_handler()
            try:
                return await execute_async(request)
            finally:
                restore_signal()

        return asyncio.run(dispatch())
    return asyncio.run(execute_async(request))
