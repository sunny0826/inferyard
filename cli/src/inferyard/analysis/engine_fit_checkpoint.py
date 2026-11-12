"""Durable in-flight checkpoints for diagnostic engine-fit runs."""

from collections import Counter
from copy import deepcopy

from inferyard.analysis.engine_fit_validation import validate_run
from inferyard.contracts.schemas_common import EXECUTION_STATES
from inferyard.contracts.validation import json_bytes
from inferyard.evidence.storage import atomic_bytes

STATES = EXECUTION_STATES


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
