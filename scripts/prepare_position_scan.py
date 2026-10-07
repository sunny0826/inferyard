"""Freeze shared-text position workloads for multiple model configurations offline."""

import argparse
from collections import defaultdict
from copy import deepcopy
from pathlib import Path

from inferyard.analysis.position import position_pattern, register_position_families
from inferyard.config.bundle import content_hash
from inferyard.config.loader import load_config
from inferyard.config.planning import write_plan
from inferyard.contracts.validation import validate_document
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    read_json,
    sha256_file,
)


def prepare(corpus, configs, out):
    bundle = read_json(corpus / "bundle.json")
    protocol = read_json(corpus / "position-protocol.json")
    validate_document("bundle", bundle)
    records, cohorts = protocol["position_cases"], protocol["cohorts"]
    case_ids = [c["case_id"] for c in bundle["cases"]]
    by_id = {r["case_id"]: r for r in records}
    if (
        len(by_id) != len(records)
        or set(by_id) != set(case_ids)
        or len(cohorts) != len(case_ids)
        or {c["case_id"] for c in cohorts} != set(case_ids)
        or protocol["body_length_unit"] != "unicode_characters_not_tokens"
    ):
        raise EvidenceError("position_scan_inventory_invalid")
    grouped = defaultdict(list)
    combinations = set()
    families = {}
    for cohort in cohorts:
        record = by_id[cohort["case_id"]]
        size = cohort["body_characters"]
        if (
            type(size) is not int
            or size <= 0
            or record["body_end"] - record["body_start"] != size
            or any(p != cohort["position"] for p in position_pattern(record)[1])
        ):
            raise EvidenceError("position_scan_cohort_mismatch")
        combination = (size, cohort["position"], record["family_id"])
        if combination in combinations:
            raise EvidenceError("position_scan_duplicate_combination")
        combinations.add(combination)
        grouped[size].append(record["case_id"])
    register_position_families(
        {"purpose": "position", "protocol": {"case_ids": case_ids}, "position_cases": records},
        bundle,
        families,
    )
    if (
        len(families) != protocol["independent_families"]
        or len(case_ids) != protocol["variant_count"]
        or combinations
        != {
            (size, position, family)
            for size in grouped
            for position in ("front", "middle", "back")
            for family in families
        }
    ):
        raise EvidenceError("position_scan_grid_incomplete")
    if not configs:
        raise EvidenceError("position_scan_requires_model_configs")
    loaded = [load_config(path).config.to_dict() for path in configs]
    if len({c["model"]["sha256"] for c in loaded}) != len(loaded):
        raise EvidenceError("position_scan_duplicate_model")
    if any(c["generation"] != loaded[0]["generation"] for c in loaded):
        raise EvidenceError("position_scan_generation_conditions_differ")
    out = out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    plans = []
    for model_index, config in enumerate(loaded):
        root = out / f"model-{model_index + 1}"
        inputs = root / "input"
        inputs.mkdir(parents=True)
        # Preserve the supplied corpus bytes exactly, including actual review records.
        atomic_bytes(inputs / "bundle.json", (corpus / "bundle.json").read_bytes())
        config = deepcopy(config)
        config["bundle"] = {"path": str(inputs / "bundle.json"), "version": bundle["version"]}
        atomic_bytes(inputs / "config.json", json_bytes(config))
        workloads = [
            {
                "workload_id": f"body-characters-{size}",
                "purpose": "position",
                "config": {"path": "config.json", "sha256": sha256_file(inputs / "config.json")},
                "bundle": {"path": "bundle.json", "sha256": sha256_file(inputs / "bundle.json")},
                "protocol": {"kind": "fixed", "case_ids": ids},
                "position_cases": [by_id[cid] for cid in ids],
                "repeats": 1,
                "timeout_seconds": config["execution"]["timeout_seconds"],
                "overhead_budget_seconds": 600,
                "input_target_tokens": None,
                "output_budget_tokens": config["generation"]["max_tokens"],
            }
            for size, ids in sorted(grouped.items())
        ]
        experiment = {
            "schema_version": 3,
            "experiment_id": f"shared-position-model-{model_index + 1}",
            "name": "Shared text position scan; body character cohorts",
            "definition_versions": {
                "measurement": "phase2.v1",
                "scoring": "phase2.v1",
                "comparison": "phase2.v1",
            },
            "execution": {
                "concurrency": 1,
                "automatic_retries": 0,
                "order": "fixed",
                "seed": None,
                "service_transition": "operator_verified",
            },
            "comparison": {"mode": "model", "factor": None},
            "budget": {
                "max_requests": len(case_ids),
                "max_wall_seconds": len(case_ids) * config["execution"]["timeout_seconds"]
                + 600 * len(workloads),
                "min_disk_bytes": config["output"]["min_disk_bytes"],
                "min_available_memory_bytes": config["output"]["min_available_memory_bytes"],
            },
            "workloads": workloads,
        }
        atomic_bytes(inputs / "experiment.json", json_bytes(experiment))
        plan = write_plan(inputs / "experiment.json", root / "frozen")
        plans.append(
            {
                "path": f"model-{model_index + 1}/frozen/plan.json",
                "sha256": sha256_file(root / "frozen/plan.json"),
                "model_sha256": config["model"]["sha256"],
                "request_limit": plan["request_limit"],
                "total_budget_seconds": plan["total_budget_seconds"],
            }
        )
    result = {
        "status": "frozen_not_executed",
        "plans": plans,
        "corpus_content_sha256": content_hash(bundle),
        "protocol_sha256": sha256_file(corpus / "position-protocol.json"),
        "independent_families": len(families),
        "variants_per_model": len(case_ids),
        "formal_request_limit": len(case_ids) * len(configs),
        "limitations": [
            "human_corpus_review_required_at_execution",
            "runtime_template_token_counts_required",
            "runtime_context_admission_required_no_truncation",
            "no_performance_comparability_claim",
        ],
    }
    atomic_bytes(out / "scan.json", json_bytes(result))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--corpus", type=Path, required=True)
    parser.add_argument("--config", type=Path, action="append", required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    print(json_bytes(prepare(args.corpus, args.config, args.out)).decode())


if __name__ == "__main__":
    main()
