"""P2-01/P2-03: independent count, lineage, missingness and budget expectations."""

import pytest
from jsonschema import Draft202012Validator

from inferyard.contracts.plan_math import plan_hash
from inferyard.config.planning import compile_plan
from inferyard.contracts.schemas import export_schema, schemas_for
from inferyard.contracts.validation import validate_document
from inferyard.errors import ContractError


def experiment():
    return {
        "schema_version": 3,
        "experiment_id": "experiment-01",
        "name": "Two repeated cases",
        "definition_versions": {"measurement": "v2", "scoring": "v1", "comparison": "v2"},
        "execution": {
            "concurrency": 1,
            "automatic_retries": 0,
            "order": "fixed",
            "seed": None,
            "service_transition": "operator_verified",
        },
        "comparison": {"mode": "model", "factor": None},
        "budget": {
            "max_requests": 100,
            "max_wall_seconds": 1000,
            "min_disk_bytes": 5 * 1024**3,
            "min_available_memory_bytes": 8 * 1024**3,
        },
        "workloads": [
            {
                "workload_id": "w1",
                "purpose": "quality",
                "config": {"path": "config.json", "sha256": "a" * 64},
                "bundle": {"path": "bundle.json", "sha256": "b" * 64},
                "protocol": {"kind": "fixed", "case_ids": ["c1", "c2"]},
                "repeats": 3,
                "timeout_seconds": 10,
                "overhead_budget_seconds": 5,
                "input_target_tokens": None,
                "output_budget_tokens": 512,
            }
        ],
    }


def observation():
    return {
        "schema_version": 3,
        "metric_id": "R01",
        "definition_version": "v2",
        "run_id": "run-1",
        "trial_id": "trial-1",
        "request_id": None,
        "group": {"workload_id": None, "category": None, "error_category": None},
        "statistic": "completion_rate",
        "value": 0.75,
        "unit": "ratio",
        "layer": "experiment",
        "source": "events.jsonl",
        "status": "derived",
        "missing_reason": None,
        "sample_count": 4,
        "numerator": 3,
        "denominator": 4,
        "excluded": 0,
        "sampled_start_ns": None,
        "sampled_end_ns": None,
        "coverage_ratio": None,
        "comparison_eligible": True,
        "limitations": [],
        "evidence_refs": [{"path": "events.jsonl", "sha256": "c" * 64}],
    }
