"""Compile single-service execution into the same frozen trial contract."""

import hashlib
import uuid

from inferyard import SCHEMA_VERSION
from inferyard.analysis.scoring import SCORER_VERSION
from inferyard.config.planning import compile_plan
from inferyard.contracts.validation import _validated_dict
from inferyard.evidence.storage import json_bytes


def compile_single_plan(config, bundle, *, experiment_id=None):
    """Freeze an in-memory single workload before any service request is sent."""
    # Document inputs contain no new information; raw public inputs still validate fully.
    config = _validated_dict("config", config)
    bundle = _validated_dict("bundle", bundle)
    count = len(bundle["cases"])
    timeout = config["execution"]["timeout_seconds"]
    auxiliary = 2 + config["execution"]["warmup_count"]
    overhead = (
        auxiliary * timeout
        + (count + auxiliary + 1) * config["execution"]["idle_wait_seconds"]
        + config["telemetry"]["baseline_seconds"]
    )
    workload = {
        "workload_id": "single",
        "purpose": "performance" if bundle.get("task_protocol") == "performance" else "quality",
        "config": {
            "path": "config.frozen.json",
            "sha256": hashlib.sha256(json_bytes(config)).hexdigest(),
        },
        "bundle": {
            "path": "bundle.json",
            "sha256": hashlib.sha256(json_bytes(bundle)).hexdigest(),
        },
        "protocol": {"kind": "fixed", "case_ids": [c["case_id"] for c in bundle["cases"]]},
        "repeats": 1,
        "timeout_seconds": timeout,
        "overhead_budget_seconds": overhead,
        "input_target_tokens": None,
        "output_budget_tokens": config["generation"]["max_tokens"],
    }
    return compile_plan(
        {
            "schema_version": SCHEMA_VERSION,
            "experiment_id": experiment_id or "single-" + uuid.uuid4().hex,
            "name": "Single service measurement",
            "definition_versions": {
                "measurement": "phase2.v1",
                "scoring": SCORER_VERSION,
                "comparison": "phase2.v1",
            },
            "execution": {
                "concurrency": 1,
                "automatic_retries": 0,
                "order": "fixed",
                "seed": None,
                "service_transition": "operator_verified",
            },
            "comparison": {"mode": "side-by-side", "factor": None},
            **(
                {"environment_admission": config["conditions"]["environment_admission"]}
                if "environment_admission" in config["conditions"]
                else {}
            ),
            "budget": {
                "max_requests": count,
                "max_wall_seconds": count * timeout + overhead,
                "min_disk_bytes": config["output"]["min_disk_bytes"],
                "min_available_memory_bytes": config["output"]["min_available_memory_bytes"],
            },
            "workloads": [workload],
        }
    )
