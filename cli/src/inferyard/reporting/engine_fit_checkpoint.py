"""Read-only recovery of durable diagnostic identity and request checkpoints."""

from inferyard.analysis.engine_fit_checkpoint import request_counts
from inferyard.analysis.engine_fit_validation import fields, require, validate_run
from inferyard.config.engine_fit import request_rows, validate_plan


def read_partial(root):
    from inferyard.reporting.engine_fit import (
        decode_json_bytes,
        manifest_present,
        read_entry,
        require_directory,
    )

    root = require_directory(root)
    require(not manifest_present(root), "partial_has_manifest")
    plan = validate_plan(decode_json_bytes(read_entry(root, "plan.json")))
    rows = decode_json_bytes(read_entry(root, "requests.json"))
    require(
        type(rows) is list
        and all(type(row) is dict and type(row.get("status")) is str for row in rows),
        "checkpoint_rows",
    )
    if (root / "checkpoint.json").exists():
        checkpoint = decode_json_bytes(read_entry(root, "checkpoint.json"))
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
        run = decode_json_bytes(read_entry(root, "run.json"))
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
