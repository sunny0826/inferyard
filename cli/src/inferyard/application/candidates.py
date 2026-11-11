"""Command result for candidate filtering, including its written evidence."""

from inferyard.application.types import CommandResult


def execute(request):
    from inferyard.analysis.candidate_filter import filter_candidates
    from inferyard.contracts.validation import json_bytes
    from inferyard.evidence.storage import atomic_bytes, new_output, read_json

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
