"""Command result for candidate filtering, including its written evidence."""

from inferyard.analysis.candidate_filter import (
    candidate_document,
    evaluate_candidate,
    validate_spec,
)
from inferyard.analysis.comparison import compare_trials
from inferyard.application.types import CommandResult
from inferyard.contracts.validation import json_bytes
from inferyard.errors import EvidenceError
from inferyard.evidence.source_locations import comparison_locations, resolve_source
from inferyard.evidence.storage import atomic_bytes, new_output, read_json
from inferyard.analysis.comparison_input import comparison_input
from inferyard.reporting.comparison_report import read_verified_comparison


def filter_candidates(spec, base):
    """Read and verify every candidate before applying the frozen thresholds."""
    validate_spec(spec)
    reference, reference_ref = comparison_input((base / spec["reference"]).resolve())
    candidates, seen = [], set()
    for path in spec["candidates"]:
        data, ref = comparison_input((base / path).resolve())
        if ref["run_id"] in seen:
            raise EvidenceError("duplicate_candidate_run")
        seen.add(ref["run_id"])
        comparison = compare_trials(reference, data)
        linked = spec.get("comparisons", {}).get(path)
        if linked is not None:
            comparison_root = (base / linked).resolve()
            comparison = read_verified_comparison(comparison_root)
            for source in comparison_locations(comparison):
                source["path"] = str(resolve_source(comparison_root, source["path"]))
            if comparison["source_runs"] != [reference_ref, ref]:
                raise EvidenceError("candidate_comparison_sources_or_order_mismatch")
        candidates.append(
            {"source": ref, **evaluate_candidate(data, comparison, spec["constraints"])}
        )
    return candidate_document(spec, reference_ref, candidates)


def execute(request):
    spec = read_json(request.filter_spec)
    result = filter_candidates(spec, request.filter_spec.parent)
    protected = [request.filter_spec.parent / p for p in [spec["reference"], *spec["candidates"]]]
    protected.extend(request.filter_spec.parent / p for p in spec.get("comparisons", {}).values())
    for candidate in result["candidates"]:
        protected.extend(
            request.filter_spec.parent / proof["source"]["path"]
            for proof in candidate["comparison"].get("performance_evidence", [])
        )
    new_output(request.out, protected)
    atomic_bytes(request.out / "candidates.json", json_bytes(result))
    return 0, CommandResult(
        request.command,
        "filtered",
        "complete",
        evidence_dir=str(request.out),
        limitations=tuple(result["limitations"]),
        details=result,
    )
