"""Read-only repeat analysis with a newly created, self-contained evidence index."""

import hashlib

from inferyard import SCHEMA_VERSION
from inferyard.analysis.repetition_analysis import make_analysis
from inferyard.analysis.repetition_metrics import repetition_metrics
from inferyard.application.types import CommandResult
from inferyard.config.plan_inputs import read_frozen_plan
from inferyard.evidence.storage import (
    EvidenceError,
    atomic_bytes,
    json_bytes,
    local_file,
    read_json,
    sha256_file,
)
from inferyard.provenance import tool_source_hash
from inferyard.runtime.batch_state import history


def write_repetition_summary(root, out):
    plan, loaded = read_frozen_plan(local_file(root, "frozen-plan/plan.json"))
    metadata = read_json(local_file(root, "batch.json"))
    if (
        metadata.get("schema_version") != SCHEMA_VERSION
        or metadata.get("plan_sha256") != plan["plan_sha256"]
    ):
        raise EvidenceError("batch_plan_identity_mismatch")
    runs = history(root, plan, loaded, allow_tool_change=True, full_verification=True)
    groups = repetition_metrics(plan, runs)
    sources = [
        {
            "run_id": d["run"]["run_id"],
            "path": str(d["path"].relative_to(root)),
            "manifest_sha256": sha256_file(d["path"] / "manifest.json"),
            "events_sha256": d["events_sha256"],
        }
        for d in runs
    ]
    result = {
        "schema_version": SCHEMA_VERSION,
        "kind": "frozen_repeat_summary.phase2.v1",
        "tool_source_sha256": tool_source_hash(),
        "plan_sha256": plan["plan_sha256"],
        "source_batch": str(root.resolve()),
        "source_runs": sources,
        "groups": groups,
        "limitations": [
            "derived_artifact_does_not_replace_source_runs",
            "performance_comparison_gate_pending",
        ],
    }
    result["analysis_files"] = [
        f"analysis-{i:04}.json" for i, group in enumerate(groups) if any(group["run_ids"])
    ]
    summary_bytes, plan_bytes = json_bytes(result), json_bytes(plan)
    refs = [
        {"path": name, "sha256": hashlib.sha256(raw).hexdigest()}
        for name, raw in (("repetition-summary.json", summary_bytes), ("plan.json", plan_bytes))
    ]
    analyses = [
        make_analysis(group, runs, plan["experiment"]["definition_versions"], refs)
        for group in groups
        if any(group["run_ids"])
    ]
    out.mkdir(parents=True, exist_ok=False)
    atomic_bytes(out / "repetition-summary.json", summary_bytes)
    atomic_bytes(out / "plan.json", plan_bytes)
    for name, analysis in zip(result["analysis_files"], analyses, strict=True):
        atomic_bytes(out / name, json_bytes(analysis))
    return result


def execute(request):
    if request.command == "repeat-check":
        result = verify_repetition_summary(request.run)
        return 0, CommandResult(request.command, "verified", "complete", details=result)
    result = write_repetition_summary(request.run, request.out)
    return 0, CommandResult(
        request.command,
        "derived",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )


def verify_repetition_summary(out):
    """Check saved analyses against the unchanged frozen batch, without writing."""
    from pathlib import Path

    from inferyard.contracts.validation import validate_document

    saved = read_json(local_file(out, "repetition-summary.json"))
    root = Path(saved["source_batch"])
    plan, loaded = read_frozen_plan(local_file(root, "frozen-plan/plan.json"))
    if (
        read_json(local_file(out, "plan.json")) != plan
        or saved["plan_sha256"] != plan["plan_sha256"]
    ):
        raise EvidenceError("repeat_analysis_plan_mismatch")
    runs = history(root, plan, loaded, allow_tool_change=True, full_verification=True)
    groups = repetition_metrics(plan, runs)
    if groups != saved["groups"]:
        raise EvidenceError("repeat_analysis_groups_changed")
    actual_sources = [
        {
            "run_id": d["run"]["run_id"],
            "path": str(d["path"].relative_to(root)),
            "manifest_sha256": sha256_file(d["path"] / "manifest.json"),
            "events_sha256": d["events_sha256"],
        }
        for d in runs
    ]
    if actual_sources != saved["source_runs"]:
        raise EvidenceError("repeat_analysis_sources_changed")
    refs = [
        {"path": name, "sha256": sha256_file(local_file(out, name))}
        for name in ("repetition-summary.json", "plan.json")
    ]
    names = [f"analysis-{i:04}.json" for i, g in enumerate(groups) if any(g["run_ids"])]
    if names != saved["analysis_files"]:
        raise EvidenceError("repeat_analysis_inventory_changed")
    for name, group in zip(names, (g for g in groups if any(g["run_ids"])), strict=True):
        analysis = read_json(local_file(out, name))
        from inferyard.evidence.formats import require_core

        require_core(analysis, "analysis")
        validate_document("analysis", analysis)
        expected = make_analysis(
            group,
            runs,
            plan["experiment"]["definition_versions"],
            refs,
            producer=saved["tool_source_sha256"],
        )
        if analysis != expected:
            raise EvidenceError("repeat_analysis_recomputation_mismatch")
    return {"verified": True, "analyses": len(names), "source_runs": len(runs)}
