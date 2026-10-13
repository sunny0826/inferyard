"""Read-only recovery of durable diagnostic identity and request checkpoints."""

from collections import Counter
from copy import deepcopy

from inferyard.config.engine_fit import request_rows, validate_plan
from inferyard.evidence.storage import atomic_bytes, json_bytes
from inferyard.reporting.engine_fit_validation import fields, require, validate_run

STATES = ("completed", "failed", "cancelled", "invalid", "not_executed")


def request_counts(rows):
    counts = Counter(row["status"] for row in rows)
    return {"planned": len(rows), **{state: counts[state] for state in STATES}}


def write_checkpoint(root, plan, run, rows):
    initial = deepcopy(run)
    initial.update(counts=request_counts(rows), stop_reason="run_in_progress")
    validate_run(plan, initial, rows)
    atomic_bytes(
        root / "checkpoint.json",
        json_bytes({"definition": "engine_fit_checkpoint.v1", "run": initial}),
    )


def read_partial(root):
    from inferyard.reporting.engine_fit import _directory, _json, _manifest_present, _read

    root = _directory(root)
    require(not _manifest_present(root), "partial_has_manifest")
    plan = validate_plan(_json(_read(root, "plan.json")))
    rows = _json(_read(root, "requests.json"))
    require(
        type(rows) is list
        and all(type(row) is dict and type(row.get("status")) is str for row in rows),
        "checkpoint_rows",
    )
    if (root / "checkpoint.json").exists():
        checkpoint = _json(_read(root, "checkpoint.json"))
        fields(checkpoint, {"definition", "run"}, "checkpoint_fields")
        require(checkpoint["definition"] == "engine_fit_checkpoint.v1", "checkpoint_definition")
        run = checkpoint["run"]
        validate_run(plan, run, request_rows(plan))
        require(
            run["completeness"] == "incomplete" and run["stop_reason"] == "run_in_progress",
            "checkpoint_state",
        )
        # This is a read projection, never a rewrite or a claim of observed final resources.
        run = {**run, "counts": request_counts(rows), "stop_reason": "interrupted_checkpoint"}
    else:
        run = _json(_read(root, "run.json"))
    validate_run(plan, run, rows)
    return {
        "kind": "engine_fit_run",
        "plan": plan,
        "run": run,
        "requests": rows,
        "integrity": "unsealed",
        "verified": False,
        "limitations": ["unsealed_checkpoint_not_final_run", "resources_only_as_saved"],
    }
