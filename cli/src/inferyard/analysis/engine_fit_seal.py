"""Publish final engine-fit snapshots and manifest after validation."""

from inferyard.analysis.engine_fit_artifacts import (
    RUN_FILES,
    entry_path,
    manifest_document,
    require_directory,
)
from inferyard.analysis.engine_fit_html import render
from inferyard.analysis.engine_fit_validation import require, validate_run
from inferyard.config.engine_fit import validate_plan
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import atomic_bytes


def seal_run(out, plan, run, rows):
    """Publish final snapshots and manifest last; an existing seal is never replaced."""
    root = require_directory(out)
    require(not (root / "manifest.json").exists(), "already_sealed")
    entry_path(root, "manifest.json")
    validate_plan(plan)
    validate_run(plan, run, rows)
    data = {"plan": plan, "run": run, "requests": rows}
    blobs = {
        "plan.json": json_bytes(plan),
        "run.json": json_bytes(run),
        "requests.json": json_bytes(rows),
        "report.html": render([data]).encode("utf-8"),
    }
    # The runtime atomically checkpoints these three JSON files before final sealing.
    for name in RUN_FILES:
        entry_path(root, name)
    require(not (root / "report.html").exists(), "report_already_exists")
    for name, content in blobs.items():
        atomic_bytes(root / name, content, overwrite=name != "report.html")
    atomic_bytes(root / "manifest.json", json_bytes(manifest_document(blobs, "run")))
    return run


