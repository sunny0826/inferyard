"""Direct offline verification without creating a derived report."""

from pathlib import Path

from inferyard.analysis.comparison_types import JSONRecord
from inferyard.application.types import CommandRequest, CommandResult
from inferyard.config.engine_fit import load_plan
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, local_file, read_json
from inferyard.runtime.batch_envelope import validate_batch_envelope


def _trial(data, sealed):
    return {
        "verified": sealed,
        "sealed": sealed,
        "integrity": "sealed" if sealed else "unsealed",
        "semantic_validation": True,
        "execution_completeness": data["summary"]["completeness"],
        "stop_reason": data["summary"]["stop_reason"],
        "run_id": data["run"]["run_id"],
        "summary": data["summary"],
        "requests": data["requests"],
    }


def _verify_batch(root: Path) -> tuple[JSONRecord, bool]:
    from inferyard.runtime.batch_state import history

    plan, loaded = read_frozen_plan(local_file(root, "frozen-plan/plan.json"))
    meta = read_json(local_file(root, "batch.json"))
    validate_batch_envelope(meta, plan["plan_sha256"])
    local_file(root, "runs")
    runs = history(root, plan, loaded, allow_tool_change=True, full_verification=True)
    trials = [_trial(data, data["manifest_sealed"]) for data in runs]
    complete = bool(trials) and all(t["verified"] for t in trials)
    missing = sorted(
        {t["trial_id"] for t in plan["trials"]} - {data["run"]["trial_id"] for data in runs}
    )
    details = {
        "verified": complete,
        "sealed": False,
        "integrity": "frozen_inputs_and_trial_seals",
        "semantic_validation": True,
        "execution_completeness": "complete"
        if not missing and all(t["execution_completeness"] == "complete" for t in trials)
        else "incomplete",
        "trials": trials,
        "planned_trials": len(plan["trials"]),
        "missing_trials": missing,
    }
    return details, complete


def execute(request: CommandRequest, kind: str) -> tuple[int, CommandResult]:
    root = request.run
    if kind == "engine-fit":
        from dataclasses import replace

        from inferyard.application.engine_fit import execute as fit

        code, result = fit(replace(request, command="engine-fit verify"))
        return code, replace(result, command=request.command)
    if kind == "plan":
        path = root if root.is_file() else local_file(root, "plan.json")
        marker = read_json(path)
        if isinstance(marker, dict) and str(marker.get("definition", "")).startswith(
            "engine_fit_plan."
        ):
            plan = load_plan(path)
            identity = plan["plan_id"]
        else:
            plan, _ = read_frozen_plan(path)
            identity = plan["plan_sha256"]
        return 0, CommandResult(
            request.command,
            "validated",
            "complete",
            details={"integrity": "frozen_document", "semantic_validation": True, "plan": identity},
        )
    if kind == "run":
        metadata = {}
        data = read_trial(root, metadata=metadata)
        details = _trial(data, metadata["manifest"] is not None)
        complete = details["verified"]
    else:
        details, complete = _verify_batch(root)
    return 0 if complete else 3, CommandResult(
        request.command,
        "verified" if complete else "partial",
        details["execution_completeness"],
        details=details,
    )
