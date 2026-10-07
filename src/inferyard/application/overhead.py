"""Public command boundary for diagnostic collector preflight and offline replay."""

import asyncio
import os

from inferyard.application.types import CommandResult
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.evidence.journal import trial_for
from inferyard.platforms.identity import PreflightError
from inferyard.registry import require_execution_support
from inferyard.runtime.overhead_runner import read_overhead, run_overhead
from inferyard.runtime.signals import install_termination_handler


def execute(request):
    if request.command == "overhead-check":
        result = (
            read_overhead(request.run, target=request.target_run)
            if request.target_run is not None
            else read_overhead(request.run)
        )
        root = request.run
    else:
        if os.name == "nt":
            raise PreflightError("windows_phase2_live_not_supported")
        plan, loaded = read_frozen_plan(request.frozen_plan)
        require_execution_support(plan, loaded)
        trial = trial_for(plan, request.trial_id)

        async def dispatch():
            restore_signal = install_termination_handler()
            try:
                return await run_overhead(
                    plan,
                    request.trial_id,
                    loaded[trial["workload_id"]],
                    request.output_root,
                    tolerance_ratio=request.tolerance_ratio,
                    max_wall_seconds=request.max_wall_seconds,
                    common_observer=request.common_observer,
                    boundary_observer=request.boundary_observer,
                    first_event_tolerance_ratio=request.first_event_tolerance_ratio,
                    engine_rate_tolerance_ratio=request.engine_rate_tolerance_ratio,
                    block_gap_tolerance_ms=request.block_gap_tolerance_ms,
                )
            finally:
                restore_signal()

        result = asyncio.run(dispatch())
        root = request.output_root
    code = result.get("execution_exit_code") or (0 if result["passed"] else 3)
    if "target_binding" in result and not result["target_binding"]["applicable"]:
        code = 3
    if "environment_binding" in result and not result["environment_binding"]["eligible"]:
        code = 3
    if any(not r["passed"] for r in result.get("first_event_assessments", {}).values()):
        code = 3
    if any(not r["passed"] for r in result.get("engine_rate_assessments", {}).values()):
        code = 3
    if any(not r["passed"] for r in result.get("block_gap_assessments", {}).values()):
        code = 3
    return code, CommandResult(
        request.command,
        "passed" if code == 0 else "not_passed",
        "complete" if result["status"] == "evaluated" else "incomplete",
        evidence_dir=str(root),
        limitations=tuple(result["limitations"]),
        details=result,
    )
