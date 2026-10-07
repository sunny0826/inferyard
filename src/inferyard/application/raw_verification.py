"""Direct offline verification without creating a derived report."""

from inferyard.application.types import CommandResult
from inferyard.config.engine_fit import load_plan
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.evidence.ledger import read_trial
from inferyard.evidence.storage import EvidenceError, local_file, read_json
from inferyard.implementation_identity import validate_identity


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


def execute(request, kind):
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
        from inferyard.runtime.batch_state import history

        plan, loaded = read_frozen_plan(local_file(root, "frozen-plan/plan.json"))
        meta = read_json(local_file(root, "batch.json"))
        from inferyard.evidence.formats import require_core

        require_core(meta, "batch")
        if (
            type(meta) is not dict
            or type(meta.get("schema_version")) is not int
            or meta["schema_version"] != 3
            or meta.get("plan_sha256") != plan["plan_sha256"]
            or type(meta.get("diagnostic")) is not bool
            or set(meta)
            - {
                "schema_version",
                "plan_sha256",
                "diagnostic",
                "tool_source_sha256",
                "implementation_identity",
            }
        ):
            raise EvidenceError("batch_plan_identity_mismatch")
        source = meta.get("tool_source_sha256")
        if (
            type(source) is not str
            or len(source) != 64
            or any(c not in "0123456789abcdef" for c in source)
        ):
            raise EvidenceError("batch_plan_identity_mismatch")
        if "implementation_identity" in meta:
            validate_identity(meta["implementation_identity"])
        local_file(root, "runs")
        runs = history(root, plan, loaded, allow_tool_change=True)
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
    return 0 if complete else 3, CommandResult(
        request.command,
        "verified" if complete else "partial",
        details["execution_completeness"],
        details=details,
    )
