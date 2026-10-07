"""Freeze a review-pending performance scan from measured preparation evidence."""

import argparse
from copy import deepcopy
from pathlib import Path

from inferyard.config.bundle import content_hash
from inferyard.config.planning import write_plan
from inferyard.contracts.validation import validate_document
from inferyard.evidence.length_evidence import read_preparation
from inferyard.evidence.storage import EvidenceError, atomic_bytes, json_bytes, sha256_file


def prepare_scan(preparations, output_budgets, repeats, out):
    if (
        not preparations
        or not output_budgets
        or len(set(output_budgets)) != len(output_budgets)
        or any(type(n) is not int or n < 1 for n in [*output_budgets, repeats])
    ):
        raise EvidenceError("length_scan_invalid_grid")
    prepared = [read_preparation(path) for path in preparations]
    first = prepared[0]["config"]
    targets = [p["spec"]["target_tokens"] for p in prepared]
    if len(set(targets)) != len(targets):
        raise EvidenceError("length_scan_duplicate_target")
    for item in prepared:
        # Runtime binding varies between preparation sessions; model/template,
        # startup and sampling conditions must be identical for this grid.
        for key in ("model", "conditions", "generation", "execution"):
            if item["config"][key] != first[key]:
                raise EvidenceError("length_scan_preparation_conditions_differ")
        engine = deepcopy(item["config"]["engine"])
        baseline = deepcopy(first["engine"])
        for value in (engine, baseline):
            args = value["startup_args"]
            if "--port" in args:
                args[args.index("--port") + 1] = "<runtime-port>"
        if engine != baseline:
            raise EvidenceError("length_scan_preparation_engine_differs")
        if (
            item["spec"]["target_tokens"] + item["spec"]["tolerance_tokens"] + max(output_budgets)
            > first["conditions"]["context_size"]
        ):
            raise EvidenceError("length_scan_context_budget_exceeded")
    policy = {
        "instruction_newlines": "crlf_to_lf",
        "strip_line_edges": True,
        "ignore_empty_lines": False,
        "unicode_normalization": "none",
        "reasoning": "separate_channel_excluded_content_unchanged",
    }
    bundle = {
        "schema_version": 3,
        "bundle_id": "prepared-length-scan",
        "version": "2.0.0-draft",
        "language": "zh-CN",
        "license_note": "Project-generated performance padding text",
        "review_records": [],
        "task_protocol": "performance",
        "answer_policy": policy,
        "cases": [
            {
                "case_id": f"length-{p['spec']['target_tokens']}",
                "category": "performance",
                "prompt": p["prompt"],
                "reference_answer": "",
                "rules": {"output_target_tokens": None},
            }
            for p in prepared
        ],
    }
    validate_document("bundle", bundle)
    out = Path(out).resolve()
    out.mkdir(parents=True, exist_ok=False)
    inputs = out / "input"
    inputs.mkdir()
    atomic_bytes(inputs / "bundle.json", json_bytes(bundle))
    workloads = []
    for item in prepared:
        target = item["spec"]["target_tokens"]
        for budget in output_budgets:
            name = f"input-{target}-output-{budget}"
            config = deepcopy(first)
            config["generation"]["max_tokens"] = budget
            config["bundle"] = {"path": str(inputs / "bundle.json"), "version": bundle["version"]}
            atomic_bytes(inputs / f"{name}.json", json_bytes(config))
            workloads.append(
                {
                    "workload_id": name,
                    "purpose": "performance",
                    "config": {
                        "path": f"{name}.json",
                        "sha256": sha256_file(inputs / f"{name}.json"),
                    },
                    "bundle": {
                        "path": "bundle.json",
                        "sha256": sha256_file(inputs / "bundle.json"),
                    },
                    "protocol": {"kind": "fixed", "case_ids": [f"length-{target}"]},
                    "repeats": repeats,
                    "timeout_seconds": config["execution"]["timeout_seconds"],
                    "overhead_budget_seconds": 600,
                    "input_target_tokens": target,
                    "input_tolerance_tokens": item["spec"]["tolerance_tokens"],
                    "output_budget_tokens": budget,
                }
            )
    experiment = {
        "schema_version": 3,
        "experiment_id": "prepared-length-scan",
        "name": "Model-specific input length and natural-stop output budget scan",
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
        "comparison": {"mode": "side-by-side", "factor": None},
        "budget": {
            "max_requests": len(workloads) * repeats,
            "max_wall_seconds": sum((w["timeout_seconds"] + 600) * repeats for w in workloads),
            "min_disk_bytes": first["output"]["min_disk_bytes"],
            "min_available_memory_bytes": first["output"]["min_available_memory_bytes"],
        },
        "workloads": workloads,
    }
    atomic_bytes(inputs / "experiment.json", json_bytes(experiment))
    write_plan(inputs / "experiment.json", out / "frozen")
    review = {
        "status": "pending_human_corpus_review",
        "bundle_content_sha256": content_hash(bundle),
        "preparations": [p["source"] for p in prepared],
        "independent_quality_questions": 0,
        "prompt_variants": len(prepared),
        "formal_requests": len(workloads) * repeats,
        "output_protocol": "natural_stop_budget_not_fixed_length",
        "limitations": [
            "requires_runtime_token_recheck",
            "requires_operator_service_handoffs",
            "model_specific_prompts",
            "no_performance_comparison_claim",
        ],
    }
    atomic_bytes(out / "review.json", json_bytes(review))
    return review


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--preparation", type=Path, action="append", required=True)
    parser.add_argument("--output-budget", type=int, action="append", required=True)
    parser.add_argument("--repeats", type=int, default=5)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    print(
        json_bytes(
            prepare_scan(args.preparation, args.output_budget, args.repeats, args.out)
        ).decode()
    )


if __name__ == "__main__":
    main()
